from django.contrib.auth import get_user_model
from djoser.serializers import SetPasswordSerializer
from rest_framework import permissions, status, viewsets
from rest_framework.decorators import action
from rest_framework.response import Response
from users.serializers import (
    UserCreateSerializer,
    UserSerializer,
)

User = get_user_model()


class UserViewSet(viewsets.ModelViewSet):
    queryset = User.objects.all()
    serializer_class = UserSerializer
    permission_classes = [permissions.AllowAny]

    def get_serializer_class(self):
        return UserSerializer if self.action in ("list", "retrieve") else UserCreateSerializer

    @action(detail=False, methods=["get", "patch"], url_path="me", permission_classes=[permissions.IsAuthenticated])
    def me(self, request):
        if request.method.lower() == "get":
            return Response(UserSerializer(request.user).data)

        serializer = UserCreateSerializer(request.user, data=request.data, partial=True)
        serializer.is_valid(raise_exception=True)
        serializer.save()
        return Response(serializer.data)

    @action(detail=False, methods=["post"], url_path="set_password", permission_classes=[permissions.IsAuthenticated])
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
