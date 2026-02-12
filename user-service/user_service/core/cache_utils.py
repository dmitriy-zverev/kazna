import hashlib

from django.core.cache import cache


def auth_fragment(request):
    auth = request.headers.get("Authorization", "")
    return hashlib.sha256(auth.encode()).hexdigest()[:16]


def user_list_cache_key(request):
    return f"user:list:{auth_fragment(request)}:{request.get_full_path()}"


def user_detail_cache_key(request, user_id):
    return f"user:detail:{user_id}:{auth_fragment(request)}"


def user_me_cache_key(request, user_id):
    return f"user:me:{user_id}:{auth_fragment(request)}"


def group_list_cache_key(request):
    return f"group:list:{auth_fragment(request)}:{request.get_full_path()}"


def group_detail_cache_key(request, user_id):
    return f"group:detail:{user_id}:{auth_fragment(request)}"


def delete_cache_patterns(patterns):
    if hasattr(cache, "delete_pattern"):
        for pattern in patterns:
            cache.delete_pattern(pattern)


def invalidate_user_cache(user_id):
    delete_cache_patterns(
        [
            "user:list:*",
            f"user:detail:{user_id}:*",
            f"user:me:{user_id}:*",
            f"group:detail:{user_id}:*",
            "group:list:*",
        ]
    )


def invalidate_group_cache(user_id):
    delete_cache_patterns(
        [
            "group:list:*",
            f"group:detail:{user_id}:*",
            f"user:detail:{user_id}:*",
            f"user:me:{user_id}:*",
        ]
    )
