.PHONY: run makemigrations migrate shell lint test test-cov
run:
	python3 user_service/manage.py runserver

makemigrations:
	python3 user_service/manage.py makemigrations

migrate:
	python3 user_service/manage.py migrate

shell:
	python3 user_service/manage.py shell

lint:
	pre-commit run --all-files

test:
	pytest tests

test-cov:
	pytest tests --cov=. --cov-config=tests/.coveragerc --cov-report term
