# Очищает описания у всех инструментов, оставляя их имена.
# Поле description у инструментов без описания не создаётся.
if type == "object" and has("tools")
then .tools |= map(if has("description") then .description = "" else . end)
else .
end
