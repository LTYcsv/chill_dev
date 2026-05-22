# Logs Service

`logs` агрегирует два типа логов:
- pipeline-логи deployment/build этапов
- runtime-логи контейнеров

## Зачем нужен сервис

В платформе есть несколько источников логов:
- `build` публикует строки сборки
- `deploy` публикует строки деплоя
- Docker хранит runtime-логи контейнеров

`logs` объединяет это в единый сервис доступа к логам, чтобы UI и CLI не ходили напрямую:
- ни в NATS
- ни в Docker
- ни в Redis

## Что делает сервис

- слушает строки логов из NATS
- хранит буфер логов по каждому deployment
- умеет стримить deployment-логи через SSE
- помечает deployment как завершенный при terminal-событиях
- может восстанавливать pipeline-логи из Redis
- читает runtime-логи контейнера через `docker logs`
- умеет follow-стрим runtime-логов

## Модель данных

### LogLine

Одна строка pipeline-лога:
- `deployment_id`
- `line`
- `source`
- `ts`

`source` обычно:
- `build`
- `deploy`
- `system`

### DeploymentLog

Буфер логов одного deployment:
- хранит ring buffer последних строк
- хранит список live SSE subscribers
- знает, завершен deployment или нет

### LogStore

In-memory registry deployment-логов:
- создает буфер по `deployment_id`
- пишет данные в Redis, если он настроен
- восстанавливает данные из Redis при первом обращении

## Pipeline logs

Pipeline-логи приходят из NATS subject'ов:
- `logs.line.*`
- `build.completed`
- `build.failed`
- `runtime.deployed`
- `deploy.failed`

Сервис делает следующее:

1. получает line event
2. распаковывает `LogLine`
3. кладет строку в deployment buffer
4. рассылает строку live subscribers
5. при наличии Redis сохраняет ее в список

Когда приходит terminal event:
- добавляет финальную summary-строку
- помечает deployment как `done`
- закрывает все live-подписки
- выставляет `done` marker в Redis

## Runtime logs

Runtime-логи не идут через NATS. Для них сервис напрямую вызывает:

`docker logs`

Предполагается, что имя контейнера совпадает с `service_id`, который задает `deploy`.

Поддерживаются два режима:
- snapshot последних строк
- live follow stream

## SSE streaming

Для deployment pipeline logs используется SSE:

`GET /api/v1/logs/deployment/{id}?follow=true`

Поведение:
- сначала отдаются уже накопленные строки
- затем идут новые строки по мере поступления
- при завершении deployment отправляется `event: done`

Это удобно для UI, где нужен live log tail без WebSocket.

## Redis persistence

Если задан `REDIS_URL`, сервис:
- сохраняет строки логов в Redis list
- сохраняет marker завершения deployment
- при первом обращении может восстановить лог из Redis

Если Redis недоступен:
- сервис продолжает работать
- логи остаются только in-memory

Это значит, что после перезапуска инстанса старые deployment logs без Redis пропадут.

## HTTP API

`GET /api/v1/logs`
- отдает runtime-логи контейнера
- query:
  `service_id` — обязателен
  `tail` — число последних строк
  `follow=true` — live stream

`GET /api/v1/logs/deployment/{id}`
- отдает pipeline-логи deployment
- query:
  `follow=true` — включить SSE streaming

## События

### Слушает

- `logs.line.*`
- `build.completed`
- `build.failed`
- `runtime.deployed`
- `deploy.failed`

### Не публикует

Сервис сам событий не публикует, он только агрегирует и отдает логи клиентам.

## Конфигурация

- `PORT`
  HTTP порт сервиса
  По умолчанию: `8085`

- `NATS_URL`
  адрес NATS
  По умолчанию: `nats://localhost:4222`

- `REDIS_URL`
  адрес Redis
  Необязателен

## Healthcheck

`GET /healthz`

Возвращает:
- статус сервиса
- состояние NATS
- состояние Redis
- число отслеживаемых deployment buffers

## Ограничения текущей реализации

- runtime-логи читаются напрямую с хоста через Docker CLI
- retention pipeline-логов в памяти ограничен `maxLinesPerDeployment`
- без Redis нет долговременного хранения
- логическая агрегация идет только на уровне `deployment_id`

## Внутренняя структура кода

- `main.go` — инициализация Redis/NATS и запуск сервера
- `config.go` — env-конфиг
- `store.go` — in-memory и Redis-backed storage для deployment logs
- `nats.go` — подписка на pipeline events
- `http.go` — HTTP API, SSE и runtime log streaming
