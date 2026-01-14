from django.contrib import admin

from .models import Group, User

admin.site.register(User)
admin.site.register(Group)
