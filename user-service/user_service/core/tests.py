from datetime import timedelta
from unittest.mock import patch

from django.contrib.auth import get_user_model
from django.urls import reverse
from rest_framework import status
from rest_framework.test import APITestCase
from rest_framework_simplejwt.tokens import AccessToken
from users.models import Group

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

    @patch("core.views.cache.delete_pattern")
    def test_user_update_clears_cache(self, delete_pattern_mock):
        url = reverse("users-detail", kwargs={"pk": self.user.pk})
        self.client.force_authenticate(user=self.user)

        response = self.client.patch(url, {"city": "Moscow"}, format="json")

        self.assertEqual(response.status_code, status.HTTP_200_OK)
        self.assertGreaterEqual(delete_pattern_mock.call_count, 1)


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

    @patch("core.views.cache.delete_pattern")
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

    @patch("core.signals.cache.delete_pattern")
    def test_user_signal_clears_cache(self, delete_pattern_mock):
        self.user.first_name = "Updated"
        self.user.save()

        self.assertGreaterEqual(delete_pattern_mock.call_count, 1)
