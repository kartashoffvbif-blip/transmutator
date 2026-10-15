// Package rules — модель правила преобразования и выбор правил
// по ключу и модели.
package rules

import "sort"

// Rule — правило в исходном виде, без скомпилированного фильтра.
type Rule struct {
	Key      string         // ключ клиента (точное совпадение, регистр важен)
	Model    string         // модель; "" — правило только по ключу (точное совпадение, регистр важен)
	Priority int            // меньше — выполняется раньше
	Order    int            // порядковый номер в манифесте; при равном priority раньше то, что выше в файле
	Filter   string         // исходный текст jq-фильтра
	Vars     map[string]any // переменные, доступные в фильтре как $имя
	Origin   string         // человекочитаемое описание для логов и ошибок
}

// Select выбирает правила для пары (ключ, модель).
//
// Порядок выбора:
//  1. если есть правила с точным совпадением ключа и модели — применяются только они;
//  2. иначе — правила, привязанные только к ключу;
//  3. если ничего не нашлось — возвращается пустой список.
//
// Результат сортируется по возрастанию priority, при равном priority —
// по порядку следования в манифесте (Order).
func Select(all []Rule, key, model string) []Rule {
	var byModel, byKey []Rule
	for _, r := range all {
		if r.Key != key {
			continue
		}
		if r.Model != "" {
			// совпадение модели строгое, с учётом регистра
			if model != "" && r.Model == model {
				byModel = append(byModel, r)
			}
			continue
		}
		byKey = append(byKey, r)
	}
	chosen := byKey
	if len(byModel) > 0 {
		chosen = byModel
	}
	out := make([]Rule, len(chosen))
	copy(out, chosen)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Order < out[j].Order
	})
	return out
}
