from django.contrib.auth.models import AbstractUser
from django.db import models

USER_TYPE_CHOICES = [
    ("buyer", "Buyer"),
    ("seller", "Seller"),
    ("moderator", "Moderator"),
    ("admin", "Admin"),
]


class User(AbstractUser):
    username = models.CharField(
        max_length=150, unique=True, blank=False, null=False, default="kazna_user"
    )
    email = models.EmailField(unique=True, blank=False, null=False, max_length=254)
    first_name = models.CharField(max_length=150)
    last_name = models.CharField(max_length=150)
    city = models.CharField(max_length=150, blank=True, null=True)
    country = models.CharField(max_length=150, blank=True, null=True)
    zip_code = models.PositiveIntegerField(blank=True, null=True)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)

    USERNAME_FIELD = "email"
    REQUIRED_FIELDS = ["username"]

    def __str__(self):
        return f"{self.username}: {self.email}"

    @property
    def is_admin(self):
        if self.is_superuser or self.is_staff:
            return True
        group = getattr(self, "group_users", None)
        return bool(group and group.user_type == "admin")

    @property
    def is_moderator(self):
        if self.is_superuser:
            return True
        group = getattr(self, "group_users", None)
        return bool(group and group.user_type == "moderator")

    @property
    def is_seller(self):
        group = getattr(self, "group_users", None)
        return bool(group and group.user_type == "seller")

    @property
    def is_buyer(self):
        group = getattr(self, "group_users", None)
        return bool(group and group.user_type == "buyer")


class Group(models.Model):
    user_type = models.CharField(
        max_length=10, choices=USER_TYPE_CHOICES, blank=False, null=False
    )
    user = models.OneToOneField(
        User,
        on_delete=models.CASCADE,
        blank=False,
        null=False,
        unique=True,
        related_name="group_users",
    )
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)

    def __str__(self):
        return f"{self.user.username}: {self.user_type}"
