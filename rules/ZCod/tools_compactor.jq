# Правило: сжимает описания инструментов в .tools.
# Для каждого инструмента:
#   1. рекурсивная очистка структуры — тримминг пробелов в концах ключей,
#      удаление ключа $schema;
#   2. замена описаний ключевых инструментов на компактные из словаря;
#   3. удаление «очевидных» описаний параметров (короткие и без
#      слов-инструкций must/should/default/...).
# Инструменты без .function (top-level name/description) обрабатываются тоже.

# 1. Рекурсивная очистка ключей: убираем пробелы в концах и удаляем $schema
def clean_structure:
  if type == "object" then
    to_entries |
    map(.key |= gsub("\\s+$"; "")) |      # Убираем пробелы в концах ключей
    map(select(.key != "$schema")) |      # Удаляем $schema
    map(.value |= clean_structure) |      # Рекурсия для вложенных объектов
    from_entries
  elif type == "array" then
    map(clean_structure)
  else
    .
  end;

# 2. Словарь сжатых описаний для ключевых инструментов
def replace_core_descriptions:
  {
    "Agent": "Launches a specialized sub-agent for complex, multi-step, or parallel tasks. Returns the agent's final message. Use SendMessage to continue. Do not use if user explicitly requests a 'workflow'.",
    "Bash": "Executes a bash command. Prefer dedicated file/search tools over shell. Avoid cat/head/tail/sed/awk unless explicitly instructed. Timeout: 120000ms (max 600000ms). Use gh CLI for GitHub ops.",
    "Read": "Reads a file from the local filesystem (returns cat -n format). Use limit/offset for large files. Do NOT re-read a file immediately after Edit/Write.",
    "Write": "Writes a file to the local filesystem, overwriting if it exists. You MUST Read the file first before overwriting. Use Edit for partial changes.",
    "Edit": "Performs exact string replacement in a file. You must Read the file first. old_string must match exactly and be unique. replace_all: true replaces every occurrence.",
    "CronCreate": "Create a persistent scheduled automation. Use cron for absolute/recurring, delayMinutes for relative delay. Prompt must describe final work directly. Never ask the run to schedule another automation.",
    "EnterPlanMode": "Transitions to plan mode to explore codebase and design implementation for user approval. Use for non-trivial tasks, multiple approaches, or architectural decisions. Skip for simple fixes.",
    "Skill": "Execute a skill within the main conversation. Set skill to exact name (no leading slash). Only invoke skills from the available-skills list or explicitly typed by user.",
    "CreateWorkflow": "Create and run a dynamic workflow (TypeScript script orchestrating subagents). MANDATORY if user explicitly asks for a 'workflow'. Load 'dynamic-workflows' skill first.",
    "AskUserQuestion": "Use only when blocked on a decision genuinely the user's to make. Use multiSelect for multiple answers. Recommend an option by making it first and adding '(Recommended)'.",
    "WebFetch": "Fetches a URL, converts to markdown, and answers prompt against it. Fails on authenticated URLs. HTTP upgraded to HTTPS. Responses cached for 15 mins.",
    "TodoWrite": "Create/update session task list. Each todo has content, status (pending|in_progress|completed), priority (high|medium|low). Send full list each call."
  } as $map |
  # имя берём из .function.name (OpenAI-формат) или .name (top-level инструмент)
  ((.function.name // .name) // "" | . as $name | $map[$name]) as $compact |
  if $compact then
    if .function then .function.description = $compact else .description = $compact end
  else .
  end;

# 3. Удаление "очевидных" описаний у параметров (короткие и без строгих указаний)
def clean_param_descriptions:
  # Работает и с .function.parameters.properties, и с .parameters.properties
  if ((.function // .).parameters.properties) then
    (if .function then .function else . end).parameters.properties |= with_entries(
      .value |= (
        if .description then
          # Если описание короче 40 символов и не содержит ключевых слов-инструкций, удаляем его
          if (.description | length) < 40 and
             (.description | test("must|should|never|always|default|deprecated|important|warn|prefer|omit|set to|true to"; "i") | not) then
            del(.description)
          else .
          end
        else .
        end
      )
    )
  else .
  end;

# === ГЛАВНАЯ ЛОГИКА ===
# Обход: каждое правило применяется к каждому инструменту по очереди;
# не-объектные элементы и пустой .tools не ломают проход.
if (.tools | type) == "array" then
  .tools |= map(
    if type == "object" then
      clean_structure |
      replace_core_descriptions |
      clean_param_descriptions
    else
      .
    end
  )
else
  .
end
