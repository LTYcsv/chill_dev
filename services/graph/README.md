# Graph Service

`graph` хранит инфраструктурный граф проекта, умеет модифицировать его, строить blast radius и восстанавливать состояние графа на момент времени.

## Зачем нужен сервис

Этот сервис нужен для визуализации и анализа инфраструктурных связей:
- какие сервисы существуют
- как они связаны друг с другом
- какие зависимости затронет падение конкретной ноды
- как менялась topology во времени

Он также умеет реагировать на runtime-события, чтобы статус нод отражал текущее состояние платформы.

## Что делает сервис

- загружает начальный граф из JSON-файла
- хранит узлы и ребра в памяти
- отдает граф по `project_id` и `environment`
- поддерживает upsert/delete нод и ребер
- вычисляет blast radius
- сохраняет diffs и checkpoints в PostgreSQL
- умеет собрать граф на конкретный момент времени
- обновляет статусы узлов по событиям из NATS

## Основные сущности

### GraphNode

Описывает узел инфраструктуры:
- `id`
- `type`
- `name`
- `project_id`
- `environment`
- `status`
- `image_tag`
- `metadata`

Типы узлов:
- `service`
- `database`
- `queue`
- `cache`
- `external`

Статусы:
- `running`
- `degraded`
- `down`
- `unknown`

### GraphEdge

Описывает связь между узлами:
- `id`
- `from`
- `to`
- `protocol`
- `rps`
- `p99_ms`
- `error_pct`

### InfraGraph

Снимок графа для пары:
- `project_id`
- `environment`

И содержит:
- `nodes`
- `edges`
- `snapshot`

### GraphDiff

Минимальная операция изменения графа:
- upsert node
- delete node
- upsert edge
- delete edge

## Источник данных

При старте сервис читает `GRAPH_CONFIG`, по умолчанию это `./graph.json`.

Этот файл содержит начальные:
- nodes
- edges

После старта граф живет в памяти, а API-операции применяются уже к in-memory состоянию.

## Time travel

Если задан `DATABASE_URL`, сервис включает режим time travel:

- сохраняет diffs каждого изменения
- периодически сохраняет checkpoints
- умеет собрать граф на конкретный timestamp
- умеет отдавать timeline последних изменений

### Как это работает

При каждом изменении графа:
1. генерируется `GraphDiff`
2. для пары `project_id + env` увеличивается sequence number
3. diff сохраняется в таблицу `tt_diffs`
4. каждый 100-й diff сохраняется checkpoint в `tt_checkpoints`

Кроме этого сервис при старте:
- проверяет, есть ли initial checkpoint
- при отсутствии сохраняет базовый checkpoint

И еще запускает периодический daily checkpoint.

### Восстановление графа на момент времени

Когда клиент вызывает `GET /api/v1/graph?at=...`, сервис:
1. ищет ближайший checkpoint до указанного времени
2. загружает из него базовый graph snapshot
3. поднимает все diffs после checkpoint до нужного времени
4. применяет их по порядку
5. возвращает восстановленный graph

## Live updates через NATS

Если доступен NATS, сервис подписывается на:
- `runtime.deployed`
- `deploy.failed`

Эти события обновляют статус соответствующего service node:
- успешный деплой переводит ноду в `running`
- failed deploy переводит ноду в `degraded`

Поиск ноды идет по:
- `node.id == service_id`
- или `node.name == service_id`
- или `node.metadata["service_id"] == service_id`

## Blast radius

Blast radius показывает, какие upstream-ноды зависят от указанной ноды.

Алгоритм:
- стартует от указанного узла
- идет по входящим ребрам
- собирает все зависимые ноды с глубиной
- сортирует результат по depth и имени

Severity определяется так:
- `low` — зависимостей нет
- `medium` — есть 1-2 зависимости
- `high` — больше 2
- `critical` — больше 5

## HTTP API

`GET /api/v1/graph`
- возвращает текущий граф
- поддерживает query-параметры `project_id`, `env`
- поддерживает `at=RFC3339` для time-travel запроса

`GET /api/v1/graph/timeline`
- возвращает последние diff-записи
- требует включенный time-travel store

`POST /api/v1/graph/nodes`
- upsert node

`DELETE /api/v1/graph/nodes/{id}`
- delete node

`POST /api/v1/graph/edges`
- upsert edge

`DELETE /api/v1/graph/edges/{id}`
- delete edge

`GET /api/v1/graph/blast-radius/{nodeID}`
- рассчитывает blast radius

## Конфигурация

- `GRAPH_CONFIG`
  путь до JSON-конфига графа
  По умолчанию: `./graph.json`

- `PORT`
  HTTP порт сервиса
  По умолчанию: `8087`

- `DATABASE_URL`
  PostgreSQL для time-travel persistence
  Если не задан, сервис работает без истории

- `NATS_URL`
  адрес NATS для live updates
  По умолчанию: `nats://localhost:4222`

## Healthcheck

`GET /healthz`

Возвращает:
- статус сервиса
- число загруженных нод
- состояние time-travel backend

## Внутренняя структура кода

- `main.go` — инициализация сервиса и зависимостей
- `config.go` — загрузка env и JSON graph config
- `domain.go` — типы графа и diff-модели
- `ttstore.go` — PostgreSQL persistence для history/checkpoints
- `server.go` — in-memory graph state и live event handling
- `query.go` — graph queries и blast radius
- `recovery.go` — восстановление графа на timestamp
- `http.go` — HTTP маршруты
