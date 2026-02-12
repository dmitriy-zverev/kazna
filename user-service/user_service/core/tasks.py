try:
    from celery import shared_task
except Exception:  # pragma: no cover

    def shared_task(*_args, **_kwargs):
        def _decorator(func):
            return func

        return _decorator


@shared_task(
    bind=True,
    autoretry_for=(Exception,),
    retry_backoff=True,
    max_retries=3,
    ignore_result=True,
)
def send_verification_email_task(self, user_id):
    from django.contrib.auth import get_user_model

    from .emailing import send_verification_email

    user = get_user_model().objects.get(pk=user_id)
    return send_verification_email(user)
