from django.contrib import admin
from .models import Tag, Video


# Register your models here.
class VideoAdmin(admin.ModelAdmin):
    readonly_fields = ("num_views", "num_likes", "published_at")


admin.site.register(Video, VideoAdmin)
admin.site.register(Tag)
