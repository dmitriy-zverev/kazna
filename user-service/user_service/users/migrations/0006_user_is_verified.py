from django.db import migrations, models


class Migration(migrations.Migration):
    dependencies = [
        ("users", "0005_alter_group_user"),
    ]

    operations = [
        migrations.AddField(
            model_name="user",
            name="is_verified",
            field=models.BooleanField(default=False),
        ),
    ]
