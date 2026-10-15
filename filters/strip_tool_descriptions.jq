# Очищает описания у всех инструментов, оставляя их имена.
# Поле description у инструментов без описания не создаётся.
.tools |= map(if has("description") then .description = "" else . end)
