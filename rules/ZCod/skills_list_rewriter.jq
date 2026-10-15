# Правило: переформатирует список скиллов в user-сообщениях.
# Если в тексте есть <system-reminder> и маркированный список
# "- Название: описание", элементы списка превращаются в компактные строки
# «• Название (alias: ...): описание» — без путей к файлам.
# Всё остальное (нет reminder, нет списка, не строка) проходит как есть.
#
# Фильтр выполняется над всем телом запроса; обрабатывается content
# каждого сообщения с ролью "user".

def rewrite_skills:
  . as $input |
  # Вход может прийти с экранированными переносами "\n" — разворачиваем
  ($input | gsub("\\\\n"; "\n") | split("\n")) as $lines |
  if ($input | contains("<system-reminder>")) and ($lines | any(test("^-\\s+[^:]+:"))) then
    $lines |
    map(
      if . == "The following skills are available for use with the Skill tool:" then
        "Available skills for the Skill tool:"
      elif test("^-\\s+[^:]+:") then
        # имя — до первого ": " (двоеточие с пробелом), чтобы не рвать
        # имена вида "browser-use:control-browser"
        (capture("^-\\s+(?<name>.+?):\\s+(?<rest>.*)$") // {name: ., rest: ""}) |
        .name as $name |
        .rest as $rest |
        (try ($rest | capture("\\(also loadable as (?<alias>[^)]+)\\)").alias) catch null) as $alias |
        ($rest | gsub("\\s*\\(also loadable as [^)]+\\)"; "") | gsub("\\s*\\(file:[^)]+\\)"; "") | gsub("\\s+$"; "")) as $desc |
        "• \($name)" + (if $alias then " (alias: \($alias))" else "" end) + ": \($desc)"
      else
        .
      end
    ) | join("\n")
  else
    $input
  end;

.messages |= map(
  if .role == "user" and (.content | type) == "string" then
    .content |= rewrite_skills
  else
    .
  end
)
