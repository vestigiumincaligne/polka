// Package genres provides localized names for standard FB2 genre codes.
package genres

// Name returns the Russian genre name for an FB2 code;
// unknown codes are returned as is.
func Name(code string) string {
	if name, ok := names[code]; ok {
		return name
	}
	return code
}

// NameLang returns the genre name in the requested language ("ru"/"en").
func NameLang(code, lang string) string {
	if lang == "en" {
		if name, ok := namesEN[code]; ok {
			return name
		}
		return code
	}
	return Name(code)
}

var names = map[string]string{
	// Science fiction and fantasy
	"sf":                 "Научная фантастика",
	"sf_action":          "Боевая фантастика",
	"sf_cyberpunk":       "Киберпанк",
	"sf_detective":       "Детективная фантастика",
	"sf_epic":            "Эпическая фантастика",
	"sf_fantasy":         "Фэнтези",
	"sf_fantasy_city":    "Городское фэнтези",
	"sf_heroic":          "Героическая фантастика",
	"sf_history":         "Альтернативная история",
	"sf_horror":          "Ужасы и мистика",
	"sf_humor":           "Юмористическая фантастика",
	"sf_postapocalyptic": "Постапокалипсис",
	"sf_social":          "Социальная фантастика",
	"sf_space":           "Космическая фантастика",
	"sf_stimpank":        "Стимпанк",
	"sf_litrpg":          "ЛитРПГ",
	"fantasy_fight":      "Боевое фэнтези",
	"love_sf":            "Любовная фантастика",
	"popadanec":          "Попаданцы",
	// Detectives and action
	"detective":           "Детектив",
	"det_action":          "Боевик",
	"det_classic":         "Классический детектив",
	"det_crime":           "Криминальный детектив",
	"det_espionage":       "Шпионский детектив",
	"det_hard":            "Крутой детектив",
	"det_history":         "Исторический детектив",
	"det_irony":           "Иронический детектив",
	"det_political":       "Политический детектив",
	"det_police":          "Полицейский детектив",
	"det_maniac":          "Про маньяков",
	"thriller":            "Триллер",
	"thriller_psychology": "Психологический триллер",
	// Prose
	"prose":              "Проза",
	"prose_classic":      "Классическая проза",
	"prose_contemporary": "Современная проза",
	"prose_counter":      "Контркультура",
	"prose_history":      "Историческая проза",
	"prose_military":     "Военная проза",
	"prose_rus_classic":  "Русская классика",
	"prose_su_classics":  "Советская классика",
	"great_story":        "Роман, повесть",
	"short_story":        "Рассказ",
	"humor_prose":        "Юмористическая проза",
	"humor":              "Юмор",
	"aphorisms":          "Афоризмы",
	// Romance
	"love":              "Любовные романы",
	"love_contemporary": "Современный любовный роман",
	"love_detective":    "Остросюжетный любовный роман",
	"love_erotica":      "Эротика",
	"love_history":      "Исторический любовный роман",
	"love_short":        "Короткий любовный роман",
	// Adventure
	"adventure":    "Приключения",
	"adv_animal":   "Про животных",
	"adv_geo":      "Путешествия и география",
	"adv_history":  "Исторические приключения",
	"adv_indian":   "Вестерн, про индейцев",
	"adv_maritime": "Морские приключения",
	"adv_western":  "Вестерн",
	// Children's
	"children":        "Детская литература",
	"child_adv":       "Детские приключения",
	"child_det":       "Детский детектив",
	"child_education": "Детская образовательная литература",
	"child_prose":     "Детская проза",
	"child_sf":        "Детская фантастика",
	"child_tale":      "Сказки",
	"child_verse":     "Детские стихи",
	"prose_game":      "Игры, упражнения для детей",
	// Poetry and drama
	"poetry":     "Поэзия",
	"dramaturgy": "Драматургия",
	"fable":      "Басни",
	"vers_libre": "Верлибры",
	// Antique
	"antique":          "Старинная литература",
	"antique_ant":      "Античная литература",
	"antique_east":     "Древневосточная литература",
	"antique_european": "Европейская старинная литература",
	"antique_myths":    "Мифы, легенды, эпос",
	"antique_russian":  "Древнерусская литература",
	"folklore":         "Фольклор",
	// Science and education
	"science":        "Научная литература",
	"sci_biology":    "Биология",
	"sci_chem":       "Химия",
	"sci_culture":    "Культурология",
	"sci_history":    "История",
	"sci_juris":      "Юриспруденция",
	"sci_linguistic": "Языкознание",
	"sci_math":       "Математика",
	"sci_medicine":   "Медицина",
	"sci_philosophy": "Философия",
	"sci_phys":       "Физика",
	"sci_politics":   "Политика",
	"sci_psychology": "Психология",
	"sci_religion":   "Религиоведение",
	"sci_tech":       "Технические науки",
	"sci_economy":    "Экономика",
	// Nonfiction
	"nonfiction":     "Документальная литература",
	"nonf_biography": "Биографии и мемуары",
	"nonf_criticism": "Критика",
	"nonf_publicism": "Публицистика",
	"nonf_military":  "Военная документалистика",
	"design":         "Искусство и дизайн",
	// Religion and esoterica
	"religion":           "Религия",
	"religion_esoterics": "Эзотерика",
	"religion_orthodoxy": "Православие",
	"religion_rel":       "Религиозная литература",
	"religion_self":      "Самосовершенствование",
	// Home, hobbies, reference
	"home":           "Домоводство",
	"home_cooking":   "Кулинария",
	"home_crafts":    "Хобби и ремёсла",
	"home_diy":       "Сделай сам",
	"home_entertain": "Развлечения",
	"home_garden":    "Сад и огород",
	"home_health":    "Здоровье",
	"home_pets":      "Домашние животные",
	"home_sex":       "Семейные отношения",
	"home_sport":     "Спорт",
	"reference":      "Справочная литература",
	"ref_dict":       "Словари",
	"ref_encyc":      "Энциклопедии",
	"ref_guide":      "Руководства",
	"ref_ref":        "Справочники",
	// Computers and business
	"computers":        "Компьютерная литература",
	"comp_db":          "Базы данных",
	"comp_hard":        "Компьютерное железо",
	"comp_osnet":       "ОС и сети",
	"comp_programming": "Программирование",
	"comp_soft":        "Программы",
	"comp_www":         "Интернет",
	"economics":        "Деловая литература",
	"job_hunting":      "Поиск работы, карьера",
	"marketing":        "Маркетинг и реклама",
	"banking":          "Финансы",
	"accounting":       "Бухучёт и налогообложение",
	"global_economy":   "Внешняя торговля",
	"paper_work":       "Делопроизводство",
	"org_behavior":     "Управление, бизнес",
	"personal_finance": "Личные финансы",
	"real_estate":      "Недвижимость",
	"small_business":   "Малый бизнес",
	"stock":            "Ценные бумаги",
	"trade":            "Торговля",
	// Other
	"periodic":           "Периодика",
	"unrecognised":       "Жанр не определён",
	"other":              "Прочее",
	"network_literature": "Сетевая литература",
	"fanfiction":         "Фанфик",
}
