# Deploy Service

`deploy` управляет жизненным циклом deployment'ов: создает deployment, инициирует сборку, реагирует на результат build-этапа и запускает контейнеры.

## Зачем нужен сервис

Это оркестратор runtime-стадии в MVP-платформе. Он связывает между собой:
- registry сервисов
- build pipeline
- Docker runtime
- secrets
- GitHub webhook'и

Именно `deploy` понимает, какой сервис к какому git-репозиторию привязан, какой порт ему нужен и в какое окружение его надо запускать.

## Что делает сервис

- регистрирует сервисы и их git-конфигурацию
- создает deployment-запись
- переводит deployment по статусам
- публикует `build.requested`
- слушает `build.completed`
- слушает `build.failed`
- получает секреты из `secrets`
- запускает контейнер через Docker
- публикует `runtime.deployed` или `deploy.failed`
- принимает GitHub push webhook и запускает автодеплой
- поддерживает rollback-событие

## Основные сущности

### ServiceConfig

Определяет, как репозиторий сопоставлен с runtime-сервисом:
- `id`
- `project_id`
- `name`
- `git_repo`
- `git_branch`
- `port`
- `environment`
- `created_at`

### Deployment

Описывает один запуск пайплайна деплоя:
- `id`
- `service_id`
- `project_id`
- `git_repo`
- `git_branch`
- `git_commit`
- `environment`
- `status`
- `triggered_by`
- `logs`
- `started_at`
- `finished_at`

### DeployRequest

Входная модель для ручного запуска deployment.

## Статусы deployment

Поддерживаются статусы:
- `queued`
- `building`
- `deploying`
- `success`
- `failed`
- `rolled_back`

Эти статусы живут в in-memory repository и используются для API-ответов и логирования.

## Поток работы

### Ручной deployment

1. Клиент вызывает `POST /api/v1/deployments`
2. Сервис создает `Deployment` со статусом `queued`
3. Публикует `build.requested`
4. Переводит deployment в `building`
5. Ждет событие `build.completed` или `build.failed`

Если приходит `build.completed`:
1. Сервис получает `image_tag`
2. Находит конфигурацию сервиса и его порт
3. Запрашивает env vars у `secrets`
4. Останавливает старый контейнер с именем `service_id`, если он существует
5. Запускает новый контейнер через `docker run`
6. Обновляет статус на `success`
7. Публикует `runtime.deployed`

Если приходит `build.failed`:
1. Статус deployment меняется на `failed`
2. В deployment добавляется текст ошибки

### Автодеплой из GitHub

1. GitHub вызывает `POST /api/v1/webhooks/github`
2. Сервис проверяет тип события
3. При наличии `WEBHOOK_SECRET` валидирует подпись `X-Hub-Signature-256`
4. Берет repo URL и branch из payload
5. Находит зарегистрированный сервис по `git_repo + git_branch`
6. Запускает обычный deployment pipeline

### Rollback

Rollback в текущем MVP не делает полноценный redeploy старого image tag. Сейчас он:
- переводит deployment в `rolled_back`
- публикует `deploy.rollback`

То есть это скорее событие для дальнейшей автоматизации, чем полный rollback-механизм.

## Реестр сервисов

`deploy` хранит registry сервисов, который связывает:
- git-репозиторий
- ветку
- порт
- окружение
- runtime service id

Это нужно для:
- автодеплоя по webhook
- понимания, какой порт пробрасывать в Docker
- получения environment-специфичных секретов

Registry может работать:
- в памяти
- в PostgreSQL, если задан `DATABASE_URL`

## Взаимодействие с secrets

Перед запуском контейнера сервис делает HTTP-запрос в `secrets`:

`GET /api/v1/secrets/env-vars?service_id=...&env_id=...`

Ответ интерпретируется как набор `KEY=VALUE`, который затем добавляется в `docker run` через `-e`.

Если `secrets` недоступен:
- deployment не прерывается
- контейнер запускается без env vars

Это осознанное поведение для более мягкого dev-режима.

## Взаимодействие с Docker

Во время runtime-стадии сервис:
- выполняет `docker stop {service_id}`
- выполняет `docker rm {service_id}`
- выполняет `docker run -d --name {service_id} ...`

Имя контейнера совпадает с `service_id`. Это важно, потому что:
- `logs` затем читает runtime-логи по этому имени
- все runtime-операции завязаны на одинаковый container naming

## События

### Слушает

- `build.completed`
- `build.failed`

### Публикует

- `build.requested`
- `logs.line.{deployment_id}`
- `runtime.deployed`
- `deploy.failed`
- `deploy.rollback`

`logs.line.{deployment_id}` используется для pipeline-логов стадии деплоя.

## HTTP API

### Deployments

`POST /api/v1/deployments`
- запускает deployment

`GET /api/v1/deployments`
- возвращает список deployment
- можно фильтровать по `service_id`

`GET /api/v1/deployments/{id}`
- возвращает один deployment

`POST /api/v1/deployments/{id}/rollback`
- инициирует rollback-событие

### Service registry

`POST /api/v1/services`
- регистрирует новый сервис в registry

`GET /api/v1/services`
- возвращает все зарегистрированные сервисы

`GET /api/v1/services/{id}`
- возвращает сервис по id

`DELETE /api/v1/services/{id}`
- удаляет сервис из registry

### Webhooks

`POST /api/v1/webhooks/github`
- принимает GitHub push webhook

## Конфигурация

- `PORT`
  HTTP порт сервиса
  По умолчанию: `8082`

- `NATS_URL`
  адрес NATS
  По умолчанию: `nats://localhost:4222`

- `WEBHOOK_SECRET`
  секрет для проверки подписи GitHub webhook
  Необязателен, но очень желателен

- `SECRETS_SVC_URL`
  адрес `secrets` сервиса
  По умолчанию: `http://localhost:8086`

- `DATABASE_URL`
  строка подключения к PostgreSQL для service registry
  Если пустая, registry будет in-memory

## Healthcheck

`GET /healthz`

Возвращает:
- статус сервиса
- подключение к NATS
- состояние PostgreSQL registry
- количество зарегистрированных сервисов

## Внутренняя структура кода

- `main.go` — инициализация зависимостей и запуск HTTP-сервера
- `config.go` — env-конфиг
- `registry.go` — service registry и PostgreSQL persistence
- `domain.go` — модели deployment и service config
- `repo.go` — in-memory repository deployment'ов
- `bus.go` — thin wrapper над NATS
- `service.go` — orchestration логика deploy pipeline
- `webhook.go` — модели и проверка подписи GitHub webhook
- `http.go` — HTTP handlers и healthcheck
