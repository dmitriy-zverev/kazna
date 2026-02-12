PYTHON ?= python
MANAGE := $(PYTHON) user_service/manage.py

.PHONY: help setup install-hooks run makemigrations migrate superuser shell db lint format test test-core test-cov check

help:
	@echo "Available targets:"
	@echo "  setup         - install dependencies and git hooks"
	@echo "  install-hooks - install pre-commit hooks for git commit flow"
	@echo "  run           - run Django dev server"
	@echo "  makemigrations/migrate/superuser/shell"
	@echo "  lint          - run pre-commit on all files"
	@echo "  format        - run formatting hooks"
	@echo "  test          - run all tests"
	@echo "  test-core     - run core tests only"
	@echo "  check         - lint + tests"

setup:
	pip install -r requirements.txt
	pre-commit install --hook-type pre-commit
	pre-commit install --hook-type commit-msg

install-hooks:
	pre-commit install --hook-type pre-commit
	pre-commit install --hook-type commit-msg

run:
	$(MANAGE) runserver

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
	pre-commit run --all-files

format:
	pre-commit run black --all-files

test:
	$(MANAGE) test

test-core:
	$(MANAGE) test core.tests

test-cov:
	pytest tests --cov=. --cov-config=tests/.coveragerc --cov-report term

check: lint test-core
