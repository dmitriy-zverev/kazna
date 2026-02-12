from django.core.cache import cache
from django.db.models.signals import post_delete, post_save
from django.dispatch import receiver
from users.models import Group, User

from .cache_utils import invalidate_group_cache, invalidate_user_cache


@receiver(post_save, sender=User)
@receiver(post_delete, sender=User)
@receiver(post_save, sender=Group)
@receiver(post_delete, sender=Group)
def invalidate_user_and_group_cache(sender, instance, **kwargs):
    if not hasattr(cache, "delete_pattern"):
        return

    if sender is User:
        invalidate_user_cache(instance.id)
    else:
        invalidate_group_cache(instance.user_id)
