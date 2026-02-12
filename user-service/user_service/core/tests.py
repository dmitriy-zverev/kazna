import hashlib
from datetime import timedelta
from types import SimpleNamespace
from unittest.mock import patch

from django.contrib.auth import get_user_model
from django.test import RequestFactory, TestCase
from django.urls import reverse
from rest_framework import status
from rest_framework.test import APITestCase
from rest_framework_simplejwt.tokens import AccessToken
from users.models import Group
from users.serializers import UserCreateSerializer

from core import cache_utils
from core.permissions import (
    IsAdminOrModerator,
    IsBuyerOrSeller,
    IsOwnerOrReadOnly,
    IsSelfOrAdmin,
    IsSellerOrReadOnly,
)

User = get_user_model()


class UserViewSetTests(APITestCase):
    def setUp(self):
        self.password = "StrongPass123!"
        self.user = User.objects.create_user(
            email="user@example.com",
            username="regular_user",
            password=self.password,
            first_name="Regular",
            last_name="User",
        )
        self.other_user = User.objects.create_user(
            email="other@example.com",
            username="other_user",
            password=self.password,
            first_name="Other",
            last_name="User",
        )
        self.admin = User.objects.create_user(
            email="admin@example.com",
            username="admin_user",
            password=self.password,
            first_name="Admin",
            last_name="User",
            is_staff=True,
        )
        self.moderator = User.objects.create_user(
            email="moderator@example.com",
            username="moderator_user",
            password=self.password,
            first_name="Moderator",
            last_name="User",
        )
        Group.objects.create(user=self.moderator, user_type="moderator")
        self.buyer = User.objects.create_user(
            email="buyer@example.com",
            username="buyer_user",
            password=self.password,
            first_name="Buyer",
            last_name="User",
        )
        Group.objects.create(user=self.buyer, user_type="buyer")

    def test_user_create_is_public(self):
        url = reverse("users-list")
        payload = {
            "email": "new@example.com",
            "username": "new_user",
            "password": "AnotherStrong123!",
            "first_name": "New",
            "last_name": "User",
        }

        response = self.client.post(url, payload, format="json")

        self.assertEqual(response.status_code, status.HTTP_201_CREATED)
        self.assertTrue(User.objects.filter(email="new@example.com").exists())

    def test_user_retrieve_requires_self_or_admin(self):
        url = reverse("users-detail", kwargs={"pk": self.other_user.pk})

        self.client.force_authenticate(user=self.user)
        forbidden_response = self.client.get(url)

        self.client.force_authenticate(user=self.admin)
        allowed_response = self.client.get(url)

        self.assertEqual(forbidden_response.status_code, status.HTTP_403_FORBIDDEN)
        self.assertEqual(allowed_response.status_code, status.HTTP_200_OK)
        self.assertEqual(allowed_response.data["id"], self.other_user.id)

    def test_users_list_requires_admin_or_moderator(self):
        url = reverse("users-list")

        self.client.force_authenticate(user=self.buyer)
        buyer_response = self.client.get(url)

        self.client.force_authenticate(user=self.moderator)
        moderator_response = self.client.get(url)

        self.client.force_authenticate(user=self.admin)
        admin_response = self.client.get(url)

        self.assertEqual(buyer_response.status_code, status.HTTP_403_FORBIDDEN)
        self.assertEqual(moderator_response.status_code, status.HTTP_200_OK)
        self.assertEqual(admin_response.status_code, status.HTTP_200_OK)

    @patch("core.cache_utils.cache.delete_pattern")
    def test_user_update_clears_cache(self, delete_pattern_mock):
        url = reverse("users-detail", kwargs={"pk": self.user.pk})
        self.client.force_authenticate(user=self.user)

        response = self.client.patch(url, {"city": "Moscow"}, format="json")

        self.assertEqual(response.status_code, status.HTTP_200_OK)
        self.assertGreaterEqual(delete_pattern_mock.call_count, 1)

    def test_me_requires_authentication(self):
        response = self.client.get(reverse("users-me"))

        self.assertEqual(response.status_code, status.HTTP_401_UNAUTHORIZED)

    def test_me_patch_requires_authentication(self):
        response = self.client.patch(
            reverse("users-me"), {"city": "Moscow"}, format="json"
        )

        self.assertEqual(response.status_code, status.HTTP_401_UNAUTHORIZED)

    def test_me_patch_updates_profile(self):
        self.client.force_authenticate(user=self.user)

        response = self.client.patch(
            reverse("users-me"), {"city": "Saint Petersburg"}, format="json"
        )

        self.assertEqual(response.status_code, status.HTTP_200_OK)
        self.user.refresh_from_db()
        self.assertEqual(self.user.city, "Saint Petersburg")

    def test_set_password_changes_credentials(self):
        self.client.force_authenticate(user=self.user)
        payload = {
            "current_password": self.password,
            "new_password": "BrandNewStrong123!",
            "re_new_password": "BrandNewStrong123!",
        }

        response = self.client.post(
            reverse("users-set-password"), payload, format="json"
        )

        self.assertEqual(response.status_code, status.HTTP_204_NO_CONTENT)

        old_login = self.client.post(
            "/api/auth/jwt/create/",
            {"email": self.user.email, "password": self.password},
            format="json",
        )
        new_login = self.client.post(
            "/api/auth/jwt/create/",
            {"email": self.user.email, "password": "BrandNewStrong123!"},
            format="json",
        )

        self.assertEqual(old_login.status_code, status.HTTP_401_UNAUTHORIZED)
        self.assertEqual(new_login.status_code, status.HTTP_200_OK)

    def test_set_password_rejects_wrong_current_password(self):
        self.client.force_authenticate(user=self.user)
        payload = {
            "current_password": "wrong-password",
            "new_password": "BrandNewStrong123!",
            "re_new_password": "BrandNewStrong123!",
        }

        response = self.client.post(
            reverse("users-set-password"), payload, format="json"
        )

        self.assertEqual(response.status_code, status.HTTP_400_BAD_REQUEST)

    def test_set_password_requires_authentication(self):
        payload = {
            "current_password": self.password,
            "new_password": "BrandNewStrong123!",
            "re_new_password": "BrandNewStrong123!",
        }

        response = self.client.post(
            reverse("users-set-password"), payload, format="json"
        )

        self.assertEqual(response.status_code, status.HTTP_401_UNAUTHORIZED)

    def test_user_delete_permissions(self):
        other_url = reverse("users-detail", kwargs={"pk": self.other_user.pk})
        self.client.force_authenticate(user=self.user)
        denied = self.client.delete(other_url)

        self.client.force_authenticate(user=self.admin)
        allowed = self.client.delete(other_url)

        self.assertEqual(denied.status_code, status.HTTP_403_FORBIDDEN)
        self.assertEqual(allowed.status_code, status.HTTP_204_NO_CONTENT)
        self.assertFalse(User.objects.filter(pk=self.other_user.pk).exists())

    def test_user_self_delete_allowed(self):
        url = reverse("users-detail", kwargs={"pk": self.user.pk})
        self.client.force_authenticate(user=self.user)

        response = self.client.delete(url)

        self.assertEqual(response.status_code, status.HTTP_204_NO_CONTENT)


class GroupViewSetTests(APITestCase):
    def setUp(self):
        self.password = "StrongPass123!"
        self.admin = User.objects.create_user(
            email="admin-group@example.com",
            username="admin_group_user",
            password=self.password,
            first_name="Admin",
            last_name="Group",
            is_staff=True,
        )
        self.moderator = User.objects.create_user(
            email="mod@example.com",
            username="moderator_user",
            password=self.password,
            first_name="Moderator",
            last_name="User",
        )
        Group.objects.create(user=self.moderator, user_type="moderator")

        self.target_user = User.objects.create_user(
            email="target@example.com",
            username="target_user",
            password=self.password,
            first_name="Target",
            last_name="User",
        )
        self.buyer = User.objects.create_user(
            email="buyer-group@example.com",
            username="buyer_group_user",
            password=self.password,
            first_name="Buyer",
            last_name="Group",
        )
        Group.objects.create(user=self.buyer, user_type="buyer")

    def test_groups_requires_authenticated_admin_or_moderator(self):
        url = reverse("groups-list")

        anonymous_response = self.client.get(url)

        self.client.force_authenticate(user=self.admin)
        admin_response = self.client.get(url)

        self.client.force_authenticate(user=self.buyer)
        buyer_response = self.client.get(url)

        self.assertEqual(anonymous_response.status_code, status.HTTP_401_UNAUTHORIZED)
        self.assertEqual(admin_response.status_code, status.HTTP_200_OK)
        self.assertEqual(buyer_response.status_code, status.HTTP_403_FORBIDDEN)

    @patch("core.cache_utils.cache.delete_pattern")
    def test_group_create_clears_cache(self, delete_pattern_mock):
        url = reverse("groups-list")
        self.client.force_authenticate(user=self.admin)

        response = self.client.post(
            url,
            {
                "user_type": "buyer",
                "user": self.target_user.pk,
            },
            format="json",
        )

        self.assertEqual(response.status_code, status.HTTP_201_CREATED)
        self.assertGreaterEqual(delete_pattern_mock.call_count, 1)

    def test_moderator_cannot_create_group(self):
        url = reverse("groups-list")
        self.client.force_authenticate(user=self.moderator)

        response = self.client.post(
            url,
            {
                "user_type": "seller",
                "user": self.target_user.pk,
            },
            format="json",
        )

        self.assertEqual(response.status_code, status.HTTP_403_FORBIDDEN)

    def test_duplicate_group_create_is_rejected(self):
        Group.objects.create(user=self.target_user, user_type="buyer")
        self.client.force_authenticate(user=self.admin)

        response = self.client.post(
            reverse("groups-list"),
            {"user_type": "seller", "user": self.target_user.pk},
            format="json",
        )

        self.assertEqual(response.status_code, status.HTTP_400_BAD_REQUEST)
        self.assertIn("user", response.data)

    def test_moderator_cannot_assign_admin_on_group_update(self):
        self.client.force_authenticate(user=self.admin)
        created = self.client.post(
            reverse("groups-list"),
            {"user_type": "buyer", "user": self.target_user.pk},
            format="json",
        )
        self.assertEqual(created.status_code, status.HTTP_201_CREATED)

        self.client.force_authenticate(user=self.moderator)
        response = self.client.patch(
            reverse("groups-detail", kwargs={"user": self.target_user.pk}),
            {"user_type": "admin"},
            format="json",
        )

        self.assertEqual(response.status_code, status.HTTP_403_FORBIDDEN)

    def test_admin_can_assign_admin_on_group_update(self):
        self.client.force_authenticate(user=self.admin)
        created = self.client.post(
            reverse("groups-list"),
            {"user_type": "buyer", "user": self.target_user.pk},
            format="json",
        )
        self.assertEqual(created.status_code, status.HTTP_201_CREATED)

        response = self.client.patch(
            reverse("groups-detail", kwargs={"user": self.target_user.pk}),
            {"user_type": "admin"},
            format="json",
        )

        self.assertEqual(response.status_code, status.HTTP_200_OK)
        self.target_user.refresh_from_db()
        self.assertEqual(self.target_user.group_users.user_type, "admin")


class AuthAndSignalsTests(APITestCase):
    def setUp(self):
        self.user = User.objects.create_user(
            email="jwt-user@example.com",
            username="jwt_user",
            password="StrongPass123!",
            first_name="Jwt",
            last_name="User",
        )

    def test_jwt_auth_endpoint_exists(self):
        response = self.client.post(
            "/api/auth/jwt/create/",
            {"email": self.user.email, "password": "StrongPass123!"},
            format="json",
        )

        self.assertEqual(response.status_code, status.HTTP_200_OK)
        self.assertIn("access", response.data)
        self.assertIn("refresh", response.data)

    def test_protected_endpoint_rejects_invalid_jwt(self):
        self.client.credentials(HTTP_AUTHORIZATION="Bearer not-a-valid-token")

        response = self.client.get(reverse("users-list"))

        self.assertEqual(response.status_code, status.HTTP_401_UNAUTHORIZED)

    def test_protected_endpoint_rejects_expired_jwt(self):
        token = AccessToken.for_user(self.user)
        token.set_exp(lifetime=timedelta(seconds=-1))
        self.client.credentials(HTTP_AUTHORIZATION=f"Bearer {str(token)}")

        response = self.client.get(reverse("users-list"))

        self.assertEqual(response.status_code, status.HTTP_401_UNAUTHORIZED)

    @patch("core.cache_utils.cache.delete_pattern")
    def test_user_signal_clears_cache(self, delete_pattern_mock):
        self.user.first_name = "Updated"
        self.user.save()

        self.assertGreaterEqual(delete_pattern_mock.call_count, 1)


class PermissionClassTests(TestCase):
    def setUp(self):
        self.permission_admin_or_mod = IsAdminOrModerator()
        self.permission_buyer_or_seller = IsBuyerOrSeller()
        self.permission_seller_or_read = IsSellerOrReadOnly()
        self.permission_self_or_admin = IsSelfOrAdmin()
        self.permission_owner_or_read = IsOwnerOrReadOnly()

    def test_is_admin_or_moderator(self):
        request = SimpleNamespace(
            user=SimpleNamespace(
                is_authenticated=True, is_admin=True, is_moderator=False
            )
        )
        self.assertTrue(self.permission_admin_or_mod.has_permission(request, None))

        request.user = SimpleNamespace(
            is_authenticated=True, is_admin=False, is_moderator=False
        )
        self.assertFalse(self.permission_admin_or_mod.has_permission(request, None))

    def test_is_buyer_or_seller(self):
        request = SimpleNamespace(
            user=SimpleNamespace(is_authenticated=True, is_buyer=True, is_seller=False)
        )
        self.assertTrue(self.permission_buyer_or_seller.has_permission(request, None))

        request.user = SimpleNamespace(
            is_authenticated=False, is_buyer=True, is_seller=True
        )
        self.assertFalse(self.permission_buyer_or_seller.has_permission(request, None))

    def test_is_seller_or_read_only(self):
        read_request = SimpleNamespace(method="GET", user=None)
        self.assertTrue(
            self.permission_seller_or_read.has_permission(read_request, None)
        )

        write_request = SimpleNamespace(
            method="POST",
            user=SimpleNamespace(is_authenticated=True, is_seller=False),
        )
        self.assertFalse(
            self.permission_seller_or_read.has_permission(write_request, None)
        )

    def test_is_self_or_admin(self):
        user = SimpleNamespace(id=1, is_authenticated=True, is_admin=False)
        request = SimpleNamespace(user=user)
        self.assertTrue(
            self.permission_self_or_admin.has_object_permission(request, None, user)
        )

        other = SimpleNamespace(id=2, is_authenticated=True, is_admin=False)
        self.assertFalse(
            self.permission_self_or_admin.has_object_permission(request, None, other)
        )

        admin_request = SimpleNamespace(
            user=SimpleNamespace(id=99, is_authenticated=True, is_admin=True)
        )
        self.assertTrue(
            self.permission_self_or_admin.has_object_permission(
                admin_request, None, other
            )
        )

    def test_is_owner_or_read_only(self):
        user = SimpleNamespace(is_admin=False)
        request = SimpleNamespace(method="PATCH", user=user)
        obj = SimpleNamespace(user=user)
        self.assertTrue(
            self.permission_owner_or_read.has_object_permission(request, None, obj)
        )

        read_request = SimpleNamespace(
            method="GET", user=SimpleNamespace(is_admin=False)
        )
        other_obj = SimpleNamespace(user=SimpleNamespace(is_admin=False))
        self.assertTrue(
            self.permission_owner_or_read.has_object_permission(
                read_request, None, other_obj
            )
        )


class UserModelRolePropertiesTests(TestCase):
    def setUp(self):
        self.base_kwargs = {
            "password": "StrongPass123!",
            "first_name": "Test",
            "last_name": "User",
        }

    def test_user_without_group_has_no_roles(self):
        user = User.objects.create_user(
            email="nogroup@example.com", username="nogroup", **self.base_kwargs
        )
        self.assertFalse(user.is_buyer)
        self.assertFalse(user.is_seller)
        self.assertFalse(user.is_moderator)

    def test_group_roles_are_reflected(self):
        user = User.objects.create_user(
            email="seller@example.com", username="selleruser", **self.base_kwargs
        )
        Group.objects.create(user=user, user_type="seller")

        self.assertTrue(user.is_seller)
        self.assertFalse(user.is_buyer)

    def test_staff_and_superuser_admin_overrides(self):
        staff = User.objects.create_user(
            email="staff@example.com",
            username="staffuser",
            is_staff=True,
            **self.base_kwargs,
        )
        superuser = User.objects.create_superuser(
            email="root@example.com",
            username="rootuser",
            password="StrongPass123!",
            first_name="Root",
            last_name="User",
        )

        self.assertTrue(staff.is_admin)
        self.assertTrue(superuser.is_admin)
        self.assertTrue(superuser.is_moderator)


class CacheUtilsTests(TestCase):
    def setUp(self):
        self.factory = RequestFactory()

    @patch("core.cache_utils.cache.delete_pattern")
    def test_invalidate_user_cache_patterns(self, delete_pattern_mock):
        cache_utils.invalidate_user_cache(42)

        self.assertEqual(delete_pattern_mock.call_count, 5)
        delete_pattern_mock.assert_any_call("user:list:*")
        delete_pattern_mock.assert_any_call("user:detail:42:*")

    def test_user_list_cache_key_contains_auth_fragment(self):
        request = self.factory.get(
            "/api/users/?limit=10", HTTP_AUTHORIZATION="Bearer abc"
        )
        key = cache_utils.user_list_cache_key(request)
        expected_fragment = hashlib.sha256("Bearer abc".encode()).hexdigest()[:16]

        self.assertIn(expected_fragment, key)
        self.assertIn("/api/users/?limit=10", key)

    @patch("core.cache_utils.cache.delete_pattern")
    def test_invalidate_group_cache_patterns(self, delete_pattern_mock):
        cache_utils.invalidate_group_cache(7)

        self.assertEqual(delete_pattern_mock.call_count, 4)
        delete_pattern_mock.assert_any_call("group:list:*")
        delete_pattern_mock.assert_any_call("group:detail:7:*")


class EmailingTests(TestCase):
    @patch("core.emailing.send_mail")
    @patch(
        "core.emailing.settings.EMAIL_VERIFICATION_URL",
        "http://example.com/verify-email",
    )
    def test_django_email_provider_sends_verification_email(self, send_mail_mock):
        user = User.objects.create_user(
            email="verify@example.com",
            username="verify_user",
            password="StrongPass123!",
            first_name="Verify",
            last_name="User",
        )

        _ = UserCreateSerializer()
        from core.emailing import DjangoEmailProvider

        DjangoEmailProvider().send_verification_email(user)

        self.assertTrue(send_mail_mock.called)
        kwargs = send_mail_mock.call_args.kwargs
        self.assertIn("Verify your email", kwargs["subject"])
        self.assertIn("http://example.com/verify-email?token=", kwargs["message"])
        self.assertEqual(kwargs["recipient_list"], [user.email])

    @patch("users.serializers.dispatch_verification_email")
    def test_user_create_serializer_triggers_verification_email(self, dispatch_mock):
        serializer = UserCreateSerializer(
            data={
                "email": "newverify@example.com",
                "username": "newverify",
                "password": "StrongPass123!",
                "first_name": "New",
                "last_name": "Verify",
            }
        )

        self.assertTrue(serializer.is_valid(), serializer.errors)
        user = serializer.save()

        self.assertTrue(User.objects.filter(pk=user.pk).exists())
        dispatch_mock.assert_called_once_with(user)

    @patch("core.emailing.send_verification_email")
    @patch("core.emailing.send_verification_email_task")
    @patch("core.emailing.settings.EMAIL_ASYNC_ENABLED", True)
    def test_dispatch_verification_email_uses_async_task_when_enabled(
        self, task_mock, sync_send_mock
    ):
        from core.emailing import dispatch_verification_email

        user = User.objects.create_user(
            email="async@example.com",
            username="async_user",
            password="StrongPass123!",
            first_name="Async",
            last_name="User",
        )

        result = dispatch_verification_email(user)

        self.assertTrue(result)
        task_mock.delay.assert_called_once_with(user.id)
        sync_send_mock.assert_not_called()

    @patch("core.emailing.send_verification_email", return_value=True)
    @patch("core.emailing.settings.EMAIL_ASYNC_ENABLED", False)
    def test_dispatch_verification_email_falls_back_to_sync_when_async_disabled(
        self, sync_send_mock
    ):
        from core.emailing import dispatch_verification_email

        user = User.objects.create_user(
            email="sync@example.com",
            username="sync_user",
            password="StrongPass123!",
            first_name="Sync",
            last_name="User",
        )

        result = dispatch_verification_email(user)

        self.assertTrue(result)
        sync_send_mock.assert_called_once_with(user)

    @patch("core.emailing.logger.warning")
    @patch("core.emailing.send_verification_email", return_value=True)
    @patch("core.emailing.settings.EMAIL_ASYNC_ENABLED", True)
    def test_dispatch_verification_email_falls_back_when_async_delay_fails(
        self, sync_send_mock, warning_mock
    ):
        from core.emailing import dispatch_verification_email

        class FailingTask:
            def delay(self, *_args, **_kwargs):
                raise RuntimeError("broker is down")

        user = User.objects.create_user(
            email="fallback@example.com",
            username="fallback_user",
            password="StrongPass123!",
            first_name="Fallback",
            last_name="User",
        )

        with patch("core.emailing.send_verification_email_task", FailingTask()):
            result = dispatch_verification_email(user)

        self.assertTrue(result)
        sync_send_mock.assert_called_once_with(user)
        warning_mock.assert_called_once()


class AsyncTaskTests(TestCase):
    @patch("core.emailing.send_verification_email", return_value=True)
    def test_send_verification_email_task_calls_sync_sender(self, send_mock):
        from core.tasks import send_verification_email_task

        user = User.objects.create_user(
            email="task@example.com",
            username="task_user",
            password="StrongPass123!",
            first_name="Task",
            last_name="User",
        )

        result = send_verification_email_task.run(user.id)

        self.assertTrue(result)
        send_mock.assert_called_once()
