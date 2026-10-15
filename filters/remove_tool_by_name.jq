# Удаляет инструмент с именем $tool_name из списка tools.
if type == "object" and has("tools")
then .tools |= map(select(.name != $tool_name))
else .
end
