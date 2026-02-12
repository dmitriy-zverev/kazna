from django.conf import settings
from django.conf.urls.static import static
from django.urls import include, path
from drf_spectacular.views import (
    SpectacularAPIView,
    SpectacularRedocView,
    SpectacularSwaggerView,
)
from rest_framework import routers

from .views import (
    EmailVerificationView,
    GroupViewSet,
    UserViewSet,
)

router = routers.DefaultRouter()
router.register(r"users", UserViewSet, basename="users")
router.register(r"groups", GroupViewSet, basename="groups")

urlpatterns = [
    path("", include(router.urls)),
    path("auth/verify-email", EmailVerificationView.as_view(), name="verify-email"),
    path("", include("djoser.urls")),
    path("auth/", include("djoser.urls.jwt")),
    path("schema/", SpectacularAPIView.as_view(), name="schema"),
    path(
        "schema/swagger-ui/",
        SpectacularSwaggerView.as_view(url_name="schema"),
        name="swagger-ui",
    ),
    path(
        "schema/redoc/", SpectacularRedocView.as_view(url_name="schema"), name="redoc"
    ),
]

if settings.DEBUG:
    urlpatterns += static(settings.MEDIA_URL, document_root=settings.MEDIA_ROOT)
