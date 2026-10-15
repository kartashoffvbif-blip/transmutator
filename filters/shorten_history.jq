# Оставляет последние N блоков «вопрос-ответ».
# N передаётся переменной $keep_last из блока vars правила.
.messages = (.messages | safe_truncate_blocks($keep_last))
