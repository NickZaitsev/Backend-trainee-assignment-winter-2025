(Я изначально случайно решил не то задание, поэтому репозиторий называется неправильно (winter-2025).. В последние три часа успел все-таки переделать autumn)

# PR Reviewer Assignment Service

Сервис автоматического назначения ревьюеров для Pull Request'ов (тестовое задание для стажёра Backend, осенняя волна 2025).

## Описание

Микросервис автоматически назначает ревьюеров на Pull Request'ы из команды автора, позволяет управлять командами и участниками через HTTP API.

## Технологический стек

- **Язык**: Go 1.23
- **База данных**: PostgreSQL 15
- **Контейнеризация**: Docker, Docker Compose

## Быстрый старт

### Запуск через Docker Compose (рекомендуется)

```bash
docker-compose up
```

Сервис будет доступен по адресу: http://localhost:8080

### Запуск для разработки

```bash
# Установить зависимости
go mod download

# Запустить PostgreSQL
docker-compose up db

# Запустить сервис
DATABASE_URL=postgres://user:password@localhost:5432/avito?sslmode=disable go run main.go
```

## API Endpoints

### Teams

- `POST /team/add` - Создать команду с участниками
- `GET /team/get?team_name=<name>` - Получить команду с участниками

### Users

- `POST /users/setIsActive` - Установить флаг активности пользователя
- `GET /users/getReview?user_id=<id>` - Получить PR'ы, где пользователь назначен ревьювером

### Pull Requests

- `POST /pullRequest/create` - Создать PR и автоматически назначить до 2 ревьюверов
- `POST /pullRequest/merge` - Пометить PR как MERGED (идемпотентная операция)
- `POST /pullRequest/reassign` - Переназначить конкретного ревьювера

Полная документация API: см. `openapi.yml`

## Дополнительные возможности (Bonus Features)

### Health Check
- `GET /health` - Проверка состояния сервиса и подключения к базе данных

Пример ответа:
```json
{
  "status": "healthy"
}
```

### Статистика
- `GET /stats` - Получить статистику использования сервиса

Возвращает:
- Общее количество команд, пользователей, PR'ов
- Количество активных пользователей
- Количество открытых и смерженных PR'ов
- Топ-10 ревьюверов с детальной статистикой

Пример ответа:
```json
{
  "total_teams": 5,
  "total_users": 42,
  "active_users": 38,
  "total_prs": 127,
  "open_prs": 23,
  "merged_prs": 104,
  "top_reviewers": [
    {
      "user_id": "u2",
      "username": "Bob",
      "review_count": 45,
      "authored_prs": 12,
      "open_reviews": 8,
      "merged_reviews": 37
    }
  ]
}
```

### Linter Configuration
Проект включает конфигурацию `golangci-lint` (`.golangci.yml`) с настройками для:
- Проверки ошибок (errcheck)
- Статического анализа (staticcheck)
- Проверки безопасности (gosec)
- Форматирования кода (gofmt, goimports)
- И многих других линтеров

Запуск линтера:
```bash
golangci-lint run
```

## Схема базы данных

```sql
teams
  - team_name (PK)

users
  - user_id (PK)
  - username
  - team_name (FK -> teams)
  - is_active

pull_requests
  - pull_request_id (PK)
  - pull_request_name
  - author_id (FK -> users)
  - status (OPEN|MERGED)
  - created_at
  - merged_at

pr_reviewers
  - pull_request_id (FK -> pull_requests)
  - user_id (FK -> users)
  - PRIMARY KEY (pull_request_id, user_id)
```

## Логика назначения ревьюверов

1. При создании PR автоматически назначаются **до двух** активных ревьюверов из команды автора (исключая самого автора)
2. Если в команде меньше доступных кандидатов, назначается доступное количество (0/1/2)
3. Выбор ревьюверов происходит случайным образом из активных участников команды
4. Пользователи с `isActive = false` не назначаются на ревью
5. При переназначении заменяется один ревьювер на случайного активного участника из команды заменяемого ревьювера
6. После `MERGED` менять список ревьюверов **нельзя**

## Принятые решения и допущения

### 1. Идемпотентность операции merge
Операция `POST /pullRequest/merge` спроектирована как идемпотентная - повторный вызов возвращает актуальное состояние PR без ошибок.

### 2. Случайный выбор ревьюверов
Выбор ревьюверов происходит случайным образом из доступных активных участников команды. Это обеспечивает равномерное распределение нагрузки по ревью.

### 3. Поведение при переназначении
При переназначении новый ревьювер выбирается из команды **заменяемого** ревьювера, а не из команды автора PR, согласно требованиям задания.

### 4. Обработка граничных случаев
- Если в команде нет активных участников кроме автора - PR создаётся с пустым списком ревьюверов
- Если при переназначении нет доступных кандидатов - возвращается ошибка `NO_CANDIDATE`

### 5. Автоматические миграции
Схема базы данных создаётся автоматически при старте приложения через `CREATE TABLE IF NOT EXISTS`.

### 6. Время ожидания базы данных
Приложение ожидает готовности базы данных до 60 секунд (30 попыток по 2 секунды), что обеспечивает корректный запуск через `docker-compose up`.

## Makefile команды

```bash
# Запуск в docker-compose
make up

# Остановка
make down

# Пересборка
make build

# Просмотр логов
make logs

# Запуск тестов
make test
```

## Примеры запросов

### Создание команды

```bash
curl -X POST http://localhost:8080/team/add \
  -H "Content-Type: application/json" \
  -d '{
    "team_name": "backend",
    "members": [
      {"user_id": "u1", "username": "Alice", "is_active": true},
      {"user_id": "u2", "username": "Bob", "is_active": true},
      {"user_id": "u3", "username": "Charlie", "is_active": true}
    ]
  }'
```

### Создание PR

```bash
curl -X POST http://localhost:8080/pullRequest/create \
  -H "Content-Type: application/json" \
  -d '{
    "pull_request_id": "pr-1001",
    "pull_request_name": "Add search feature",
    "author_id": "u1"
  }'
```

### Переназначение ревьювера

```bash
curl -X POST http://localhost:8080/pullRequest/reassign \
  -H "Content-Type: application/json" \
  -d '{
    "pull_request_id": "pr-1001",
    "old_user_id": "u2"
  }'
```

## Производительность

- Целевой RPS: 5
- SLI времени ответа: 300 мс
- SLI успешности: 99.9%

Текущая реализация легко справляется с заявленными требованиями при объёме данных до 20 команд и 200 пользователей.

Решение тестового задания для стажировки Backend в Avito (осень 2025)
