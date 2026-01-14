.PHONY: run makemigrations migrate superuser shell db lint test test-cov
run:
	python3 user_service/manage.py runserver

makemigrations:
	python3 user_service/manage.py makemigrations

migrate:
	python3 user_service/manage.py migrate

superuser:
	python3 user_service/manage.py createsuperuser

shell:
	python3 user_service/manage.py shell

db:
	psql -d kazna_user_service

lint:
	pre-commit run --all-files

test:
	pytest tests

test-cov:
	pytest tests --cov=. --cov-config=tests/.coveragerc --cov-report term
