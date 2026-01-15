from django.contrib.auth import get_user_model
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

from .permissions import IsAdminOrModerator

User = get_user_model()


class UserViewSet(viewsets.ModelViewSet):
    queryset = User.objects.all()
    serializer_class = UserSerializer
    permission_classes = [permissions.AllowAny]

    def get_serializer_class(self):
        return (
            UserSerializer
            if self.action in permissions.SAFE_METHODS
            else UserCreateSerializer
        )

    @action(
        detail=False,
        methods=["get", "patch"],
        url_path="me",
        permission_classes=[permissions.IsAuthenticated],
    )
    def me(self, request):
        if self.action in permissions.SAFE_METHODS:
            return Response(UserSerializer(request.user).data)

        serializer = UserSerializer(request.user, data=request.data, partial=True)
        serializer.is_valid(raise_exception=True)
        serializer.save()
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

        return Response(status=status.HTTP_204_NO_CONTENT)


class GroupViewSet(viewsets.ModelViewSet):
    queryset = Group.objects.all()
    serializer_class = GroupSerializer
    lookup_field = "user"
    permission_classes = [IsAdminOrModerator]
    http_method_names = ["get", "post", "patch"]

    def perform_create(self, serializer):
        if self.request.user.is_moderator:
            raise exceptions.PermissionDenied(
                {"detail": "You cannot perform this action"}
            )
        user = get_object_or_404(User, pk=self.request.data["user"])
        if Group.objects.filter(user=user):
            raise exceptions.ValidationError({"user": "Already exists"})
        serializer.save(user=user)

    def perform_update(self, serializer):
        if self.request.user.is_moderator and self.request.data["user_type"] == "admin":
            raise exceptions.PermissionDenied("You cannot assign admins")
        serializer.save()
