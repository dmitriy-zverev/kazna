import logging
from urllib.parse import quote

from django.conf import settings
from django.core.mail import send_mail
from django.core.signing import TimestampSigner
from django.utils.module_loading import import_string

logger = logging.getLogger(__name__)


class BaseEmailProvider:
    def send_verification_email(self, user):
        raise NotImplementedError


class DjangoEmailProvider(BaseEmailProvider):
    def send_verification_email(self, user):
        signer = TimestampSigner(salt="user-email-verification")
        token = signer.sign(str(user.pk))
        encoded_token = quote(token)
        base_url = settings.EMAIL_VERIFICATION_URL.rstrip("/")
        verification_url = f"{base_url}?token={encoded_token}"

        send_mail(
            subject="Verify your email",
            message=(
                f"Hi {user.first_name or user.username},\n\n"
                f"Please verify your email using this link:\n{verification_url}\n\n"
                "If you did not create this account, please ignore this email."
            ),
            from_email=settings.DEFAULT_FROM_EMAIL,
            recipient_list=[user.email],
            fail_silently=False,
        )


def get_email_provider():
    provider_cls = import_string(settings.EMAIL_PROVIDER_CLASS)
    return provider_cls()


def send_verification_email(user):
    try:
        get_email_provider().send_verification_email(user)
        return True
    except Exception:
        logger.exception(
            "Failed to send verification email", extra={"user_id": user.id}
        )
        return False
