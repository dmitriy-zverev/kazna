import re

from django.contrib.auth import get_user_model
from djoser.serializers import (
    UserCreateSerializer as BaseUserCreateSerializer,
)
from djoser.serializers import (
    UserSerializer as BaseUserSerializer,
)
from rest_framework import serializers
from rest_framework.validators import UniqueValidator

User = get_user_model()


class UserCreateSerializer(BaseUserCreateSerializer):
    first_name = serializers.CharField(required=True, allow_blank=False, max_length=150)
    last_name = serializers.CharField(required=True, allow_blank=False, max_length=150)
    username = serializers.CharField(
        required=True,
        allow_blank=False,
        max_length=150,
        validators=[
            UniqueValidator(queryset=User.objects.all(), message="User already exists")
        ],
    )

    class Meta(BaseUserCreateSerializer.Meta):
        model = User
        fields = ("id", "email", "username", "password", "first_name", "last_name")
        extra_kwargs = {
            "password": {
                "write_only": True,
                "min_length": 8,
                "max_length": 150,
            },
        }

    def validate_username(self, value):
        if not re.match(r"^[\w.@+-]+\Z", value):
            raise serializers.ValidationError(
                {"username": "Contains invalid characters"}
            )
        return value


class UserSerializer(BaseUserSerializer):
    class Meta(BaseUserSerializer.Meta):
        model = User
        fields = ("id", "email", "username", "first_name", "last_name")
