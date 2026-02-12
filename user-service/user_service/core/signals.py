from django.core.cache import cache
from django.db.models.signals import post_delete, post_save
from django.dispatch import receiver
from users.models import Group, User


@receiver(post_save, sender=User)
@receiver(post_delete, sender=User)
@receiver(post_save, sender=Group)
@receiver(post_delete, sender=Group)
def invalidate_user_and_group_cache(sender, instance, **kwargs):
    if not hasattr(cache, "delete_pattern"):
        return

    if sender is User:
        user_id = instance.id
        patterns = [
            "user:list:*",
            f"user:detail:{user_id}:*",
            f"user:me:{user_id}:*",
            f"group:detail:{user_id}:*",
            "group:list:*",
        ]
    else:
        user_id = instance.user_id
        patterns = [
            "group:list:*",
            f"group:detail:{user_id}:*",
            f"user:detail:{user_id}:*",
            f"user:me:{user_id}:*",
        ]

    for pattern in patterns:
        cache.delete_pattern(pattern)
