PYTHON ?= uv run python
MANAGE := $(PYTHON) user_service/manage.py
CELERY_APP := user_service.celery:app
CELERY_PYTHONPATH := /Users/Dmitriy/workspace/github.com/kazna/user-service/user_service

.PHONY: help setup install-hooks run celery-worker celery-beat smoke-async-email smoke-async-e2e makemigrations migrate superuser shell db lint format test test-core test-cov check

help:
	@echo "Available targets:"
	@echo "  setup         - install dependencies and git hooks"
	@echo "  install-hooks - install pre-commit hooks for git commit flow"
	@echo "  run           - run Django dev server"
	@echo "  celery-worker - run Celery worker"
	@echo "  celery-beat   - run Celery beat scheduler"
	@echo "  smoke-async-email - run end-to-end async email smoke command"
	@echo "  smoke-async-e2e - one-command RabbitMQ + worker + async smoke flow"
	@echo "  makemigrations/migrate/superuser/shell"
	@echo "  lint          - run pre-commit on all files"
	@echo "  format        - run formatting hooks"
	@echo "  test          - run all tests"
	@echo "  test-core     - run core tests only"
	@echo "  check         - lint + tests"

setup:
	uv sync --dev
	uv run pre-commit install --hook-type pre-commit
	uv run pre-commit install --hook-type commit-msg

install-hooks:
	uv run pre-commit install --hook-type pre-commit
	uv run pre-commit install --hook-type commit-msg

run:
	$(MANAGE) runserver

celery-worker:
	PYTHONPATH=$(CELERY_PYTHONPATH) uv run celery -A $(CELERY_APP) worker --loglevel=info

celery-beat:
	PYTHONPATH=$(CELERY_PYTHONPATH) uv run celery -A $(CELERY_APP) beat --loglevel=info

smoke-async-email:
	$(MANAGE) smoke_async_email --timeout=30

smoke-async-e2e:
	@set -euo pipefail; \
	cd /Users/Dmitriy/workspace/github.com/kazna && docker compose up -d rabbitmq; \
	for i in $$(seq 1 60); do \
		STATUS=$$(docker inspect -f '{{.State.Health.Status}}' kazna-rabbitmq 2>/dev/null || echo "unknown"); \
		if [ "$$STATUS" = "healthy" ]; then \
			echo "RabbitMQ is healthy"; \
			break; \
		fi; \
		if [ $$i -eq 60 ]; then \
			echo "RabbitMQ did not become healthy in time"; \
			exit 1; \
		fi; \
		sleep 1; \
	done; \
	cd /Users/Dmitriy/workspace/github.com/kazna/user-service; \
	rm -f /tmp/kazna-celery-worker.log; \
	PYTHONPATH=$(CELERY_PYTHONPATH) uv run celery -A $(CELERY_APP) worker --loglevel=info --pool=solo >/tmp/kazna-celery-worker.log 2>&1 & \
	WORKER_PID=$$!; \
	cleanup() { kill $$WORKER_PID >/dev/null 2>&1 || true; }; \
	trap cleanup EXIT INT TERM; \
	for i in $$(seq 1 30); do \
		if ! kill -0 $$WORKER_PID >/dev/null 2>&1; then \
			echo "Celery worker exited unexpectedly"; \
			tail -n 100 /tmp/kazna-celery-worker.log || true; \
			exit 1; \
		fi; \
		if grep -qi "ready" /tmp/kazna-celery-worker.log; then \
			echo "Celery worker is ready"; \
			break; \
		fi; \
		if [ $$i -eq 30 ]; then \
			echo "Celery worker did not become ready in time"; \
			tail -n 100 /tmp/kazna-celery-worker.log || true; \
			exit 1; \
		fi; \
		sleep 1; \
	done; \
	EMAIL_ASYNC_ENABLED=true $(MANAGE) smoke_async_email --timeout=30

makemigrations:
	$(MANAGE) makemigrations

migrate:
	$(MANAGE) migrate

superuser:
	$(MANAGE) createsuperuser

shell:
	$(MANAGE) shell

db:
	psql -d kazna_user_service

lint:
	uv run pre-commit run --all-files

format:
	uv run pre-commit run black --all-files

test:
	$(MANAGE) test

test-core:
	$(MANAGE) test core.tests

test-cov:
	uv run pytest tests --cov=. --cov-config=tests/.coveragerc --cov-report term

check: lint test-core
