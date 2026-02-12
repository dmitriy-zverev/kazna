import hashlib

from django.contrib.auth import get_user_model
from django.core.cache import cache
from django.shortcuts import get_object_or_404
from djoser.serializers import SetPasswordSerializer
from rest_framework import exceptions, permissions, status, viewsets
from rest_framework.decorators import action
from rest_framework.response import Response
from users.models import Group
from users.serializers import (
    GroupSerializer,
    UserCreateSerializer,
    UserSerializer,
)

from .permissions import IsAdminOrModerator, IsSelfOrAdmin

User = get_user_model()


class UserViewSet(viewsets.ModelViewSet):
    queryset = User.objects.all()
    serializer_class = UserSerializer
    permission_classes = [permissions.IsAuthenticated]

    def _auth_fragment(self, request):
        auth = request.headers.get("Authorization", "")
        return hashlib.sha256(auth.encode()).hexdigest()[:16]

    def _user_list_cache_key(self, request):
        return f"user:list:{self._auth_fragment(request)}:{request.get_full_path()}"

    def _user_detail_cache_key(self, request, user_id):
        return f"user:detail:{user_id}:{self._auth_fragment(request)}"

    def _user_me_cache_key(self, request, user_id):
        return f"user:me:{user_id}:{self._auth_fragment(request)}"

    def _delete_cache_patterns(self, patterns):
        if hasattr(cache, "delete_pattern"):
            for pattern in patterns:
                cache.delete_pattern(pattern)

    def list(self, request, *args, **kwargs):
        key = self._user_list_cache_key(request)
        cached = cache.get(key)
        if cached is not None:
            return Response(cached)

        response = super().list(request, *args, **kwargs)
        cache.set(key, response.data, 60 * 10)
        return response

    def retrieve(self, request, *args, **kwargs):
        user_id = kwargs.get("pk")
        key = self._user_detail_cache_key(request, user_id)
        cached = cache.get(key)
        if cached is not None:
            return Response(cached)

        response = super().retrieve(request, *args, **kwargs)
        cache.set(key, response.data, 60 * 10)
        return response

    def _invalidate_user_cache(self, user_id):
        self._delete_cache_patterns(
            [
                "user:list:*",
                f"user:detail:{user_id}:*",
                f"user:me:{user_id}:*",
            ]
        )

    def get_serializer_class(self):
        return UserCreateSerializer if self.action == "create" else UserSerializer

    def get_permissions(self):
        if self.action == "create":
            return [permissions.AllowAny()]
        if self.action in ("retrieve", "update", "partial_update", "destroy"):
            return [permissions.IsAuthenticated(), IsSelfOrAdmin()]
        return [permission() for permission in self.permission_classes]

    def perform_create(self, serializer):
        instance = serializer.save()
        self._invalidate_user_cache(instance.id)

    def perform_update(self, serializer):
        instance = serializer.save()
        self._invalidate_user_cache(instance.id)

    def perform_destroy(self, instance):
        user_id = instance.id
        instance.delete()
        self._invalidate_user_cache(user_id)

    @action(
        detail=False,
        methods=["get", "patch"],
        url_path="me",
        permission_classes=[permissions.IsAuthenticated],
    )
    def me(self, request):
        if request.method in permissions.SAFE_METHODS:
            key = self._user_me_cache_key(request, request.user.id)
            cached = cache.get(key)
            if cached is not None:
                return Response(cached)

            data = UserSerializer(request.user).data
            cache.set(key, data, 60 * 10)
            return Response(data)

        serializer = UserSerializer(request.user, data=request.data, partial=True)
        serializer.is_valid(raise_exception=True)
        instance = serializer.save()
        self._invalidate_user_cache(instance.id)
        return Response(serializer.data)

    @action(
        detail=False,
        methods=["post"],
        url_path="set_password",
        permission_classes=[permissions.IsAuthenticated],
    )
    def set_password(self, request):
        serializer = SetPasswordSerializer(
            data=request.data,
            context={"request": request},
        )
        serializer.is_valid(raise_exception=True)

        new_password = serializer.validated_data["new_password"]

        user = request.user
        user.set_password(new_password)
        user.save()
        self._invalidate_user_cache(user.id)

        return Response(status=status.HTTP_204_NO_CONTENT)


class GroupViewSet(viewsets.ModelViewSet):
    queryset = Group.objects.all()
    serializer_class = GroupSerializer
    lookup_field = "user"
    permission_classes = [IsAdminOrModerator]
    http_method_names = ["get", "post", "patch"]

    def _auth_fragment(self, request):
        auth = request.headers.get("Authorization", "")
        return hashlib.sha256(auth.encode()).hexdigest()[:16]

    def _group_list_cache_key(self, request):
        return f"group:list:{self._auth_fragment(request)}:{request.get_full_path()}"

    def _group_detail_cache_key(self, request, user_id):
        return f"group:detail:{user_id}:{self._auth_fragment(request)}"

    def _delete_cache_patterns(self, patterns):
        if hasattr(cache, "delete_pattern"):
            for pattern in patterns:
                cache.delete_pattern(pattern)

    def list(self, request, *args, **kwargs):
        key = self._group_list_cache_key(request)
        cached = cache.get(key)
        if cached is not None:
            return Response(cached)

        response = super().list(request, *args, **kwargs)
        cache.set(key, response.data, 60 * 10)
        return response

    def retrieve(self, request, *args, **kwargs):
        user_id = kwargs.get("user")
        key = self._group_detail_cache_key(request, user_id)
        cached = cache.get(key)
        if cached is not None:
            return Response(cached)

        response = super().retrieve(request, *args, **kwargs)
        cache.set(key, response.data, 60 * 10)
        return response

    def _invalidate_group_cache(self, user_id):
        self._delete_cache_patterns(
            [
                "group:list:*",
                f"group:detail:{user_id}:*",
            ]
        )

    def perform_create(self, serializer):
        if self.request.user.is_moderator:
            raise exceptions.PermissionDenied(
                {"detail": "You cannot perform this action"}
            )
        user = get_object_or_404(User, pk=self.request.data["user"])
        if Group.objects.filter(user=user):
            raise exceptions.ValidationError({"user": "Already exists"})
        instance = serializer.save(user=user)
        self._invalidate_group_cache(instance.user_id)

    def perform_update(self, serializer):
        if self.request.user.is_moderator and self.request.data["user_type"] == "admin":
            raise exceptions.PermissionDenied("You cannot assign admins")
        instance = serializer.save()
        self._invalidate_group_cache(instance.user_id)
