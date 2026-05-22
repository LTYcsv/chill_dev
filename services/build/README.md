# Build Service

`build` отвечает за сборку контейнерного образа сервиса из git-репозитория и публикацию результата в registry.

## Зачем нужен сервис

Этот сервис отделяет стадию сборки от стадии деплоя. `deploy` не должен сам заниматься клонированием репозитория и вызовами `docker build`; вместо этого он публикует событие о сборке, а `build` выполняет пайплайн и возвращает результат через NATS.

За счет этого:
- сборка становится самостоятельным этапом
- логи build-процесса можно стримить отдельно
- в будущем проще выделить build-worker'ы в отдельные инстансы

## Что делает сервис

- принимает запрос на сборку через HTTP
- слушает событие `build.requested` из NATS
- клонирует репозиторий по `git_repo` и `git_branch`
- проверяет наличие `Dockerfile`
- генерирует дефолтный `Dockerfile`, если его нет
- запускает `docker build`
- пытается запушить образ в registry
- публикует построчные build-логи в NATS
- публикует итоговые события `build.completed` или `build.failed`

## Основная сущность

`BuildRequest`
- `deployment_id`
- `service_id`
- `git_repo`
- `git_branch`
- `environment`

`deployment_id` используется как связка между стадией сборки, логами и дальнейшим деплоем.

## Поток работы

### Build через событие

1. `deploy` публикует `build.requested`
2. `build` получает сообщение из NATS
3. Создает рабочую директорию `/tmp/build-{deployment_id}`
4. Выполняет `git clone --depth=1 --branch ...`
5. Проверяет наличие `Dockerfile`
6. Если файла нет, подставляет шаблон по типу проекта
7. Выполняет `docker build`
8. Выполняет `docker push`
9. Публикует `build.completed`

### Build через HTTP

1. Клиент вызывает `POST /api/v1/builds`
2. Сервис запускает тот же пайплайн асинхронно
3. Сразу отвечает `202 Accepted`

## Генерация Dockerfile

Если в репозитории нет `Dockerfile`, сервис пробует определить стек проекта:

- если найден `go.mod`
  генерируется Dockerfile для Go

- если найден `package.json`
  генерируется Dockerfile для Node.js

- иначе
  используется базовый шаблон для Python

Это dev-friendly поведение, чтобы сервис мог подхватывать простые проекты даже без ручной контейнеризации.

## Логи

Сервис стримит stdout и stderr сборочных команд:
- `git clone`
- `docker build`
- `docker push`

Каждая строка публикуется в subject:

`logs.line.{deployment_id}`

Полезная нагрузка содержит:
- `deployment_id`
- `line`
- `source=build`
- timestamp

Это позволяет сервису `logs` собирать unified pipeline log.

## События

### Слушает

- `build.requested`

### Публикует

- `logs.line.{deployment_id}`
- `build.completed`
- `build.failed`

`build.completed` содержит:
- `deployment_id`
- `image_tag`

`build.failed` содержит:
- `deployment_id`
- `error`

## Важная особенность push

Если `docker push` завершается ошибкой, сервис не валит весь build в dev-режиме. Вместо этого:
- пишет warning в лог
- не возвращает ошибку наружу

Это сделано для локальной разработки, где registry может быть недоступен.

## HTTP API

`POST /api/v1/builds`
- запускает сборку асинхронно

Пример тела:

```json
{
  "deployment_id": "dep-123",
  "service_id": "svc-123",
  "git_repo": "https://github.com/org/repo.git",
  "git_branch": "main",
  "environment": "production"
}
```

## Зависимости

### NATS

Используется для:
- получения задач на сборку
- публикации логов
- публикации статусов build-этапа

### Docker

Нужен для:
- `docker build`
- `docker push`
- проверки доступности Docker в healthcheck

### Git

Используется для клонирования исходного репозитория.

### Registry

Адрес registry задается через `REGISTRY_HOST`.

Финальный image tag формируется так:

`{registry_host}/{service_id}:{deployment_id_prefix}`

## Конфигурация

- `PORT`
  HTTP порт сервиса
  По умолчанию: `8083`

- `NATS_URL`
  адрес NATS
  По умолчанию: `nats://localhost:4222`

- `REGISTRY_HOST`
  адрес Docker registry
  По умолчанию: `localhost:5000`

## Healthcheck

`GET /healthz`

Возвращает:
- статус сервиса
- состояние NATS
- registry host
- наличие Docker daemon

## Внутренняя структура кода

- `main.go` — запуск сервиса и подключение зависимостей
- `config.go` — env-конфиг
- `builder.go` — основной build-пайплайн
- `worker.go` — подписка на `build.requested`
- `http.go` — HTTP маршруты
- `system.go` — системные проверки, например Docker detection
