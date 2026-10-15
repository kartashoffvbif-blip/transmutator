# Удаляет инструмент с именем $tool_name из списка tools.
.tools |= map(select(.name != $tool_name))
