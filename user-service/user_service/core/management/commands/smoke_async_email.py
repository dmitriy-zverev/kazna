import time
import uuid
from pathlib import Path

from core.emailing import dispatch_verification_email
from django.conf import settings
from django.contrib.auth import get_user_model
from django.core.management.base import BaseCommand, CommandError


class Command(BaseCommand):
    help = "End-to-end smoke test for async email dispatch via Celery + broker"

    def add_arguments(self, parser):
        parser.add_argument("--timeout", type=int, default=30)

    def handle(self, *args, **options):
        if not getattr(settings, "EMAIL_ASYNC_ENABLED", False):
            raise CommandError("EMAIL_ASYNC_ENABLED must be true for async smoke test")

        timeout = options["timeout"]
        email_dir = Path(settings.EMAIL_FILE_PATH)
        email_dir.mkdir(parents=True, exist_ok=True)

        user_id_suffix = uuid.uuid4().hex[:8]
        email = f"smoke-{user_id_suffix}@example.com"
        username = f"smoke_{user_id_suffix}"

        user_model = get_user_model()
        user = user_model.objects.create_user(
            email=email,
            username=username,
            password="SmokePass123!",
            first_name="Smoke",
            last_name="Test",
        )

        try:
            started_at = time.time()
            dispatch_verification_email(user)

            deadline = started_at + timeout
            while time.time() < deadline:
                for path in sorted(
                    email_dir.glob("*"), key=lambda p: p.stat().st_mtime, reverse=True
                ):
                    try:
                        if path.stat().st_mtime < started_at:
                            continue
                        content = path.read_text(encoding="utf-8", errors="ignore")
                    except OSError:
                        continue
                    if email in content and "Verify your email" in content:
                        self.stdout.write(
                            self.style.SUCCESS("Async email smoke test passed")
                        )
                        self.stdout.write(f"Email file: {path}")
                        return
                time.sleep(1)

            raise CommandError("Timed out waiting for async verification email output")
        finally:
            user.delete()
