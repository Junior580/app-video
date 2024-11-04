package converter

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

type VideoConverter struct {
	db *sql.DB
}

func NewVideoConverter(db *sql.DB, rootPath string) *VideoConverter {
	return &VideoConverter{
		// rabbitClient: rabbitClient,
		db: db,
		// rootPath:     rootPath,
	}
}

type VideoTask struct {
	VideoID int    `json:"video_id"`
	Path    string `json:"path"`
}

func (vc *VideoConverter) handle(msg []byte) {
	var task VideoTask
	err := json.Unmarshal(msg, &task)
	if err != nil {
		vc.logError(task, "failed to unmarshal task", err)
	}

	if IsProcessed(vc.db, task.VideoID) {
		slog.Warn("Video already processed", slog.Int("video_id", task.VideoID))
		// d.Ack(false)
		return
	}

	err = vc.processVideo(&task)
	if err != nil {
		vc.logError(task, "Error during video conversion", err)
		// d.Ack(false)
		return
	}
	slog.Info("Video conversion processed", slog.Int("video_id", task.VideoID))

	err = MarkProcessed(vc.db, task.VideoID)
	if err != nil {
		vc.logError(task, "Failed to mark video as processed", err)
	}
	// d.Ack(false)
	slog.Info("Video marked as processed", slog.Int("video_id", task.VideoID))

}

func (vc *VideoConverter) processVideo(task *VideoTask) error {
	chunkPath := filepath.Join(vc.rootPath, fmt.Sprintf("%d", task.VideoID))
	mergedFile := filepath.Join(chunkPath, "merged.mp4")
	mpegDashPath := filepath.Join(chunkPath, "mpeg-dash")

	slog.Info("Merging chunks", slog.String("path", chunkPath))
	if err := vc.mergeChunks(chunkPath, mergedFile); err != nil {
		return fmt.Errorf("failed to merge chunks: %v", err)
	}

	err := os.MkdirAll(mpegDashPath, os.ModePerm)
	if err != nil {
		vc.logError(*task, "failed to create mpeg-dash directory", err)
		return err
	}

	ffmpegCmd := exec.Command(
		"ffmpeg", "-i", mergedFile, // Arquivo de entrada
		"-f", "dash", // Formato de saída
		filepath.Join(mpegDashPath, "output.mpd"), // Caminho para salvar o arquivo .mpd
	)

	output, err := ffmpegCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to convert to MPEG-DASH: %v, output: %s", err, string(output))
	}
	slog.Info("Converted to MPEG-DASH", slog.String("path", mpegDashPath))

	if err := os.Remove(mergedFile); err != nil {
		slog.Warn("Failed to remove merged file", slog.String("file", mergedFile), slog.String("error", err.Error()))
	}
	slog.Info("Removed merged file", slog.String("file", mergedFile))

	return nil

}

func (vc *VideoConverter) extractNumber(fileName string) int {
	re := regexp.MustCompile(`\d+`)
	numStr := re.FindString(filepath.Base(fileName))
	num, err := strconv.Atoi(numStr)

	if err != nil {
		return -1
	}

	return num
}

func (vc *VideoConverter) mergeChunks(inputDir, outputFile string) error {
	chunks, err := filepath.Glob(filepath.Join(inputDir, "*.chunk"))

	if err != nil {
		return fmt.Errorf("failed to find chunks: %v", err)
	}

	sort.Slice(chunks, func(i, j int) bool {
		return vc.extractNumber(chunks[i]) < vc.extractNumber(chunks[j])
	})

	output, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create a output file: %v", err)
	}

	defer output.Close()

	for _, chunk := range chunks {
		input, err := os.Open(chunk)
		if err != nil {
			return fmt.Errorf("failed to open chunk: %v", err)
		}

		_, err = output.ReadFrom(input)
		if err != nil {
			return fmt.Errorf("failed to write %s to merged file: %v", chunk, err)
		}
		input.Close()
	}
	return nil
}

func (vc *VideoConverter) logError(task VideoTask, message string, err error) {
	errorData := map[string]any{
		"video_id": task.VideoID,
		"error":    message,
		"details":  err.Error(),
		"time":     time.Now(),
	}

	serializedError, _ := json.Marshal(errorData)
	slog.Error("Processing error", slog.String("error_details", string(serializedError)))

	RegisterError(vc.db, errorData, err)
}
