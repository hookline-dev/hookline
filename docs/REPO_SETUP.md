# REPO_SETUP.md — настройка репозитория и доски (делает лид, один раз, ~2–3 часа)

Пошаговая инструкция. Идти сверху вниз, ничего не пропуская. В конце — чек-лист приёмки.

Почему репозиторий **публичный**: GitHub Actions на публичных репозиториях бесплатны и без лимита минут (на приватных Free-плане даётся всего 2000 минут в месяц). Плюс это портфолио каждого участника — ссылку можно вставлять в резюме.

---

## Шаг 0. Организация (10 мин)

1. Создай организацию: github.com → `+` → New organization → Free.
   Имя, например, `hookline-dev`. Организация, а не личный аккаунт: доска и репозиторий будут общими, а не «твоими».
2. People → Invite member → пригласи всех четверых.
3. **Требование ко всем:** включена 2FA (Settings → Password and authentication). Без 2FA в организацию не добавляем.
4. Организация → Settings → Member privileges: Base permissions = **Write** (могут пушить ветки, но не в `main` — его защитим отдельно).

## Шаг 1. Репозиторий (10 мин)

1. New repository → `hookline`, **Public**, добавить README, лицензию **MIT**, `.gitignore` = Go.
2. Settings → General:
   - Pull Requests: оставить только **Allow squash merging**, снять галки с merge commits и rebase merging.
   - Включить **Automatically delete head branches** (ветки после мержа удаляются сами).
   - Включить Issues и Projects (по умолчанию включены).
3. Settings → Collaborators and teams → добавить всех с ролью **Write**.

## Шаг 2. Защита `main` (ruleset) — самое важное (15 мин)

Settings → Rules → Rulesets → **New branch ruleset**:

- Name: `protect-main`; Enforcement status: **Active**
- Target branches: Add target → **Include default branch**
- Bypass list: **пусто** (даже лид не обходит правила — это принципиально)
- Включить правила:
  - ✅ Restrict deletions
  - ✅ Block force pushes
  - ✅ Require linear history
  - ✅ Require a pull request before merging
    - Required approvals: **1**
    - ✅ Dismiss stale approvals when new commits are pushed
    - ✅ Require conversation resolution before merging
  - ✅ Require status checks to pass → добавить (после первого прогона CI): `build`, `lint`, `test`
    - ✅ Require branches to be up to date before merging

Проверка: попробуй `git push` прямо в main — должен получить отказ. Если пуш прошёл — ruleset не активен, чини.

## Шаг 3. Метки (labels) (10 мин)

Issues → Labels. Удалить дефолтный мусор (`duplicate`, `wontfix`, `invalid`, `question`), оставить `bug`, `documentation`, `enhancement` и создать:

| Метка | Цвет | Смысл |
|---|---|---|
| `good-first-issue` | зелёный | безопасная задача для новичка (уже есть у GitHub — просто используем) |
| `blocked` | красный | человек застрял, разбираем на планировании первым |
| `flaky` | оранжевый | нестабильный тест, чиним в тот же спринт |
| `money`… нет, у нас: `delivery-bug` | красный | баг доставки: нужен `message_id` и вывод `GET /messages/{id}` |
| `core` | фиолетовый | queue/worker/signing — нужны 2 аппрува |
| `chore` | серый | инфраструктура, CI, конфиги |

Быстрый способ через `gh` CLI:
```bash
gh label create blocked --color B60205 --description "Исполнитель застрял, обсудить первым"
gh label create flaky --color D93F0B --description "Нестабильный тест"
gh label create delivery-bug --color B60205 --description "Баг доставки: нужен message_id"
gh label create core --color 5319E7 --description "queue/worker/signing: 2 аппрува"
gh label create chore --color BFBFBF --description "Инфраструктура и конфиги"
```

## Шаг 4. Шаблоны issue и PR (20 мин)

Создать файлы в репозитории:

**`.github/ISSUE_TEMPLATE/task.yml`**
```yaml
name: Задача
description: Обычная задача разработки
labels: []
body:
  - type: textarea
    id: what
    attributes:
      label: Что
      description: Одним предложением — что должно появиться или измениться
    validations: { required: true }
  - type: input
    id: why
    attributes:
      label: Зачем
      description: Какой раздел TZ.md это закрывает (например, §5.3)
    validations: { required: true }
  - type: textarea
    id: acceptance
    attributes:
      label: Критерии приёмки
      value: |
        - [ ] 
        - [ ] 
    validations: { required: true }
  - type: textarea
    id: where
    attributes:
      label: Где в коде
      description: Пакеты и файлы, которые предстоит трогать
  - type: textarea
    id: hints
    attributes:
      label: Подсказки
      description: Ссылки на доки, похожий код в репо. Обязательно для L0–L1.
```

**`.github/ISSUE_TEMPLATE/delivery_bug.yml`**
```yaml
name: Баг доставки
description: Сообщение доставилось не так, как ожидалось
labels: ["bug", "delivery-bug"]
body:
  - type: input
    id: message_id
    attributes: { label: message_id }
    validations: { required: true }
  - type: textarea
    id: dump
    attributes:
      label: Вывод GET /messages/{id}
      render: json
    validations: { required: true }
  - type: textarea
    id: expected
    attributes: { label: Ожидал / Получил }
    validations: { required: true }
```

**`.github/pull_request_template.md`**
```markdown
Closes #

## Что сделано

## Как проверить
```bash
# команды, которые должен выполнить ревьюер
```

## Чек-лист
- [ ] `make lint` и `make test` зелёные локально
- [ ] Новая бизнес-логика покрыта table-driven тестами
- [ ] PR ≤ 300 изменённых строк
- [ ] Нет `time.Now()` в бизнес-логике и `time.Sleep` в тестах
- [ ] Обновлён openapi.yaml / документация (если менялся контракт)
```

**`.github/CODEOWNERS`** (владелец модуля автоматически становится ревьюером):
```
# По умолчанию — лид
*                       @lead-nick

/internal/queue/        @lead-nick
/internal/worker/       @lead-nick
/internal/ingest/       @intern-nick
/internal/delivery/     @intern-nick
/internal/breaker/      @intern-nick
/internal/signing/      @dev-a
/internal/backoff/      @dev-a
/internal/matcher/      @dev-a
/internal/api/          @dev-b
/web/                   @dev-b
/internal/attempts/     @dev-c
/cmd/sink/              @dev-c
/docs/                  @dev-c
```

## Шаг 5. Доска GitHub Projects (30 мин)

1. Организация → Projects → **New project** → шаблон **Board** → имя `Hookline`.
2. Settings проекта → Manage access → добавить всю команду с ролью **Write**.
3. Привязать репозиторий: в проекте `⋯` → Settings → Manage repositories (или просто добавлять issue из репо — они привяжутся).

### Поля (Settings → Fields → New field)

| Поле | Тип | Значения |
|---|---|---|
| `Status` | Single select (есть по умолчанию) | Backlog, Ready, In Progress, In Review, Done |
| `Level` | Single select | L0, L1, L2, L3, L4 |
| `Size` | Single select | S, M, L |
| `Sprint` | **Iteration** | длительность 1 неделя, старт — понедельник; сгенерировать 8 итераций |
| `Module` | Single select | queue, worker, ingest, delivery, signing, backoff, matcher, breaker, attempts, api, web, infra, docs |

`Sprint` делаем именно типом **Iteration** (а не текстом) — тогда GitHub сам подсвечивает текущую итерацию и показывает, что в неё попало.

### Автоматизация (проект → `⋯` → Workflows)

Включить готовые воркфлоу:
- **Item added to project** → Status = `Backlog`
- **Item reopened** → Status = `In Progress`
- **Pull request opened** (linked issue) → Status = `In Review`
- **Item closed** → Status = `Done`
- **Auto-add to project**: репозиторий `hookline`, фильтр `is:issue is:open` — все новые issue сами падают на доску.

Это те самые «руки не нужны»: задача сама уезжает в `In Review` при открытии PR и в `Done` при мерже (потому что в PR стоит `Closes #123`).

### Сохранённые вью (вкладки внутри проекта)

Создай пять вкладок — команда будет жить в них, а не в общей каше:

| Вкладка | Тип | Фильтр |
|---|---|---|
| `🗂 Доска` | Board, группировка по Status | `-status:Done` |
| `📥 Ready` | Table | `status:Ready` sort by Level |
| `🏃 Спринт` | Board | `sprint:@current` |
| `🙋 Мои` | Table | `assignee:@me -status:Done` |
| `🔥 Blocked` | Table | `label:blocked` |

## Шаг 6. CI (30 мин)

`.github/workflows/ci.yml` — три джоба с именами, которые ты указал в ruleset (`build`, `lint`, `test`):

```yaml
name: CI
on:
  pull_request:
  push: { branches: [main] }

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26', cache: true }
      - run: go build ./...
      - run: go vet ./...

  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26', cache: true }
      - run: go test -race -coverprofile=coverage.out ./...

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26', cache: true }
      - uses: golangci/golangci-lint-action@v8
        with: { version: v2.12.2 }

  secrets:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: gitleaks/gitleaks-action@v2
```

Отдельным воркфлоу — проверка Conventional Commits в заголовке PR (`amannn/action-semantic-pull-request` или аналог).

После первого прогона вернись в ruleset (Шаг 2) и добавь `build`, `test`, `lint` в Required status checks — до первого прогона GitHub их просто не покажет в списке.

## Шаг 7. Секреты и безопасность (10 мин)

- Settings → Secrets and variables → Actions: сюда кладём токены (например, для деплоя демо). В код — никогда.
- Settings → Code security: включить **Secret scanning** и **Push protection** (для публичных репо бесплатно) — GitHub сам заблокирует пуш с токеном.
- Включить **Dependabot alerts** и **security updates**.
- В `.gitignore` обязательно: `.env`, `*.pem`, `coverage.out`.

## Шаг 8. Наполнение бэклога (60 мин, самое ценное)

1. Создать issue по фазам 0–2 из `ROADMAP.md`: одна строка роадмапа = 1–3 issue. Минимум **25 штук** до старта.
2. Каждому issue проставить: `Level`, `Size`, `Module`, ответственного (или оставить свободным).
3. Из `STARTER_TASKS.md` завести **все задачи блоков 1–3** и повесить метку `good-first-issue`. К понедельнику в `Ready` должно лежать минимум 6–8 таких задач: каждый новичок должен иметь возможность взять свою первую задачу, ни у кого не спрашивая.
4. Верх бэклога (то, что берём в S1) перевести в `Ready`, остальное оставить в `Backlog`.
5. **Обязательные issue первой недели:** ADR-0003 «очередь на SKIP LOCKED», ADR-0004 «Clock-интерфейс», миграция 0001, скелет проекта, docs/onboarding.md.

Массовое создание через CLI (быстрее, чем кликать):
```bash
gh issue create --title "queue: claim-запрос на FOR UPDATE SKIP LOCKED" \
  --body-file .github/drafts/queue-claim.md --label core
```

## Шаг 9. Документы в репозиторий

Положить в `docs/`: `TZ.md`, `ROADMAP.md`, `GITHUB_PROJECTS_GUIDE.md`, `GIT_FOR_BEGINNERS.md`, `STARTER_TASKS.md`, `onboarding.md`, `adr/`. `CONTRIBUTING.md` и `README.md` — в корень (GitHub сам покажет CONTRIBUTING при создании PR — приятный бонус).

В README сверху — ссылки на эти документы и на доску проекта.

---

## Чек-лист приёмки (пройди сам, прежде чем звать команду)

- [ ] Пуш в `main` напрямую отклоняется — у тебя тоже
- [ ] PR без аппрува и с красным CI смержить нельзя
- [ ] Новый issue автоматически появляется на доске в `Backlog`
- [ ] Тестовый PR со строкой `Closes #N` при открытии двигает карточку в `In Review`, при мерже — в `Done` и закрывает issue
- [ ] Ветка после мержа удаляется сама
- [ ] В `Ready` лежит ≥ 6 задач с меткой `good-first-issue` и заполненными Level/Size
- [ ] Все пятеро приняли инвайт, у всех включена 2FA
- [ ] Все прочитали `GITHUB_PROJECTS_GUIDE.md` и (новички) `GIT_FOR_BEGINNERS.md`

Когда все галочки стоят — проект готов принимать людей. Первый понедельник = первое планирование, спринт S1 стартует.
