package httpserver

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Instruction представляет инструкцию по применению
type Instruction struct {
	GUIDInstruction    string  `json:"guid_instruction"`
	IDInstruction      *int64  `json:"id_instruction,omitempty"`
	Instruction        *string `json:"instruction,omitempty"`
	Composition        *string `json:"composition,omitempty"`
	Indication         *string `json:"indication,omitempty"`
	Dosage             *string `json:"dosage,omitempty"`
	Lactation          *string `json:"lactation,omitempty"`
	PharmAction        *string `json:"pharm_action,omitempty"`
	ContraIndication   *string `json:"contra_indication,omitempty"`
	SideEffect         *string `json:"side_effect,omitempty"`
	Interaction        *string `json:"interaction,omitempty"`
	OverDosage         *string `json:"over_dosage,omitempty"`
	SpecialInstruction *string `json:"special_instruction,omitempty"`
	GoodsDesc          *string `json:"goods_desc,omitempty"`
	StoringCondition   *string `json:"storing_condition,omitempty"`
}

// handleGetDrugAnalogsAndSynonyms возвращает аналоги и синонимы препарата
func (s *Server) handleGetDrugAnalogsAndSynonyms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	guidES := r.URL.Query().Get("guid_es")
	if guidES == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан guid_es")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Сначала получаем информацию о препарате
	var currentINN sql.NullString
	var currentTRN sql.NullString
	getDrugQuery := `
		SELECT INN_NAME_RUS, TRN_NAME_RUS
		FROM es_ef2
		WHERE GUID_ES = CAST(@guidES AS UUID)
		  AND DELETED IS NULL
	`
	err := s.database.GORMWith(ctx).Raw(getDrugQuery, sql.Named("guidES", guidES)).Row().Scan(&currentINN, &currentTRN)
	if err != nil {
		if err == sql.ErrNoRows {
			s.writeError(w, http.StatusNotFound, "Препарат не найден")
			return
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения препарата: %v", err))
		return
	}

	type DrugInfo struct {
		GUID_ES      string  `json:"guid_es"`
		Name         string  `json:"name"`
		TradeName    *string `json:"trade_name,omitempty"`
		INN          *string `json:"inn,omitempty"`
		CureForm     *string `json:"cure_form,omitempty"`
		ProducerName *string `json:"producer_name,omitempty"`
		Dosage       *string `json:"dosage,omitempty"`
		Barcode      *string `json:"barcode,omitempty"`
		RegistryPrice *float64 `json:"registry_price,omitempty"`
		HasPrice     bool    `json:"has_price"` // Есть ли цена в прайсах
	}

	var analogs []DrugInfo
	var synonyms []DrugInfo

	// Получаем аналоги (препараты с таким же МНН, но другой GUID_ES)
	if currentINN.Valid && currentINN.String != "" && strings.TrimSpace(currentINN.String) != "" {
		analogsQuery := `
			SELECT CAST(ef2.GUID_ES AS TEXT) AS GUID_ES,
				ef2.NAME,
				ef2.TRN_NAME_RUS,
				ef2.INN_NAME_RUS,
				ef2.CUREFORM_NAME,
				ep.PRODUCER_NAME,
				ef2.DOSAGE,
				ef2.BARCODE,
				ef2.REESTR_PRICE,
				CASE WHEN sp.SupplierPriceID IS NOT NULL THEN 1 ELSE 0 END AS HasPrice
			FROM es_ef2 ef2
			LEFT JOIN es_producer ep ON ef2.PRODUCER_COD = ep.KOD_PRODUCER
			LEFT JOIN SupplierPrice sp ON ef2.GUID_ES = sp.GUID_ES AND sp.IsActive = 1
			WHERE ef2.INN_NAME_RUS = @innName
			  AND ef2.GUID_ES != CAST(@guidES AS UUID)
			  AND (ef2.DELETED IS NULL)
			ORDER BY ef2.NAME, ep.PRODUCER_NAME
LIMIT 20
`
		
		if s.logger != nil {
			s.logger.Debug("Поиск аналогов для МНН: %s, GUID_ES: %s", currentINN.String, guidES)
		}
		
		rows, err := s.database.GORMWith(ctx).Raw(analogsQuery,
			sql.Named("innName", strings.TrimSpace(currentINN.String)),
			sql.Named("guidES", guidES)).Rows()
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Ошибка получения аналогов: %v", err)
			}
		} else {
			defer rows.Close()
			for rows.Next() {
				var drug DrugInfo
				var tradeName, inn, cureForm, producerName, dosage, barcode sql.NullString
				var registryPrice sql.NullFloat64
				var hasPrice sql.NullInt64

				err := rows.Scan(&drug.GUID_ES, &drug.Name, &tradeName, &inn, &cureForm,
					&producerName, &dosage, &barcode, &registryPrice, &hasPrice)
				if err != nil {
					continue
				}

				if tradeName.Valid {
					drug.TradeName = &tradeName.String
				}
				if inn.Valid {
					drug.INN = &inn.String
				}
				if cureForm.Valid {
					drug.CureForm = &cureForm.String
				}
				if producerName.Valid {
					drug.ProducerName = &producerName.String
				}
				if dosage.Valid {
					drug.Dosage = &dosage.String
				}
				if barcode.Valid {
					drug.Barcode = &barcode.String
				}
				if registryPrice.Valid {
					drug.RegistryPrice = &registryPrice.Float64
				}
				drug.HasPrice = hasPrice.Valid && hasPrice.Int64 > 0

				analogs = append(analogs, drug)
			}
		}
	}

	// Получаем синонимы (разные торговые наименования или похожие записи)
	// Синонимы - это препараты с похожим названием или тот же препарат от другого производителя
	if currentTRN.Valid && currentTRN.String != "" {
		// Ищем по торговому наименованию (TRN_NAME_RUS)
		synonymsQuery := `
			SELECT CAST(ef2.GUID_ES AS TEXT) AS GUID_ES,
				ef2.NAME,
				ef2.TRN_NAME_RUS,
				ef2.INN_NAME_RUS,
				ef2.CUREFORM_NAME,
				ep.PRODUCER_NAME,
				ef2.DOSAGE,
				ef2.BARCODE,
				ef2.REESTR_PRICE,
				CASE WHEN sp.SupplierPriceID IS NOT NULL THEN 1 ELSE 0 END AS HasPrice
			FROM es_ef2 ef2
			LEFT JOIN es_producer ep ON ef2.PRODUCER_COD = ep.KOD_PRODUCER
			LEFT JOIN SupplierPrice sp ON ef2.GUID_ES = sp.GUID_ES AND sp.IsActive = 1
			WHERE (
				-- Похожее торговое наименование или имя
				(ef2.TRN_NAME_RUS IS NOT NULL AND ef2.TRN_NAME_RUS LIKE @trnPattern)
				OR (ef2.NAME LIKE @namePattern)
			)
			  AND ef2.GUID_ES != CAST(@guidES AS UUID)
			  AND ef2.DELETED IS NULL
			ORDER BY 
				CASE WHEN ef2.TRN_NAME_RUS = @currentTRN THEN 1 ELSE 2 END,
				ef2.NAME
LIMIT 20
`
		
		// Создаем паттерны для поиска
		trnPattern := strings.TrimSpace(currentTRN.String) + "%"
		namePattern := "%" + strings.TrimSpace(currentTRN.String) + "%"
		
		rows, err := s.database.GORMWith(ctx).Raw(synonymsQuery,
			sql.Named("trnPattern", trnPattern),
			sql.Named("namePattern", namePattern),
			sql.Named("currentTRN", currentTRN.String),
			sql.Named("guidES", guidES)).Rows()
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Ошибка получения синонимов: %v", err)
			}
		} else {
			defer rows.Close()
			for rows.Next() {
				var drug DrugInfo
				var tradeName, inn, cureForm, producerName, dosage, barcode sql.NullString
				var registryPrice sql.NullFloat64
				var hasPrice sql.NullInt64

				err := rows.Scan(&drug.GUID_ES, &drug.Name, &tradeName, &inn, &cureForm,
					&producerName, &dosage, &barcode, &registryPrice, &hasPrice)
				if err != nil {
					continue
				}

				if tradeName.Valid {
					drug.TradeName = &tradeName.String
				}
				if inn.Valid {
					drug.INN = &inn.String
				}
				if cureForm.Valid {
					drug.CureForm = &cureForm.String
				}
				if producerName.Valid {
					drug.ProducerName = &producerName.String
				}
				if dosage.Valid {
					drug.Dosage = &dosage.String
				}
				if barcode.Valid {
					drug.Barcode = &barcode.String
				}
				if registryPrice.Valid {
					drug.RegistryPrice = &registryPrice.Float64
				}
				drug.HasPrice = hasPrice.Valid && hasPrice.Int64 > 0

				synonyms = append(synonyms, drug)
			}
		}
	}

	// Если не нашли синонимы по торговому наименованию, ищем по похожему названию
	if len(synonyms) == 0 {
		var currentName sql.NullString
		getNameQuery := `SELECT NAME FROM es_ef2 WHERE GUID_ES = CAST(@guidES AS UUID)`
		if err := s.database.GORMWith(ctx).Raw(getNameQuery, sql.Named("guidES", guidES)).Row().Scan(&currentName); err == nil && currentName.Valid {
			synonymsQuery := `
				SELECT CAST(ef2.GUID_ES AS TEXT) AS GUID_ES,
					ef2.NAME,
					ef2.TRN_NAME_RUS,
					ef2.INN_NAME_RUS,
					ef2.CUREFORM_NAME,
					ep.PRODUCER_NAME,
					ef2.DOSAGE,
					ef2.BARCODE,
					ef2.REESTR_PRICE,
					CASE WHEN sp.SupplierPriceID IS NOT NULL THEN 1 ELSE 0 END AS HasPrice
				FROM es_ef2 ef2
				LEFT JOIN es_producer ep ON ef2.PRODUCER_COD = ep.KOD_PRODUCER
				LEFT JOIN SupplierPrice sp ON ef2.GUID_ES = sp.GUID_ES AND sp.IsActive = 1
				WHERE ef2.NAME LIKE @namePattern
				  AND ef2.GUID_ES != CAST(@guidES AS UUID)
				  AND ef2.DELETED IS NULL
				ORDER BY ef2.NAME
LIMIT 10
`
			
			namePattern := "%" + strings.TrimSpace(currentName.String) + "%"
			rows, err := s.database.GORMWith(ctx).Raw(synonymsQuery,
				sql.Named("namePattern", namePattern),
				sql.Named("guidES", guidES)).Rows()
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var drug DrugInfo
					var tradeName, inn, cureForm, producerName, dosage, barcode sql.NullString
					var registryPrice sql.NullFloat64
					var hasPrice sql.NullInt64

					if rows.Scan(&drug.GUID_ES, &drug.Name, &tradeName, &inn, &cureForm,
						&producerName, &dosage, &barcode, &registryPrice, &hasPrice) == nil {
						if tradeName.Valid {
							drug.TradeName = &tradeName.String
						}
						if inn.Valid {
							drug.INN = &inn.String
						}
						if cureForm.Valid {
							drug.CureForm = &cureForm.String
						}
						if producerName.Valid {
							drug.ProducerName = &producerName.String
						}
						if dosage.Valid {
							drug.Dosage = &dosage.String
						}
						if barcode.Valid {
							drug.Barcode = &barcode.String
						}
						if registryPrice.Valid {
							drug.RegistryPrice = &registryPrice.Float64
						}
						drug.HasPrice = hasPrice.Valid && hasPrice.Int64 > 0

						synonyms = append(synonyms, drug)
					}
				}
			}
		}
	}

	if analogs == nil {
		analogs = []DrugInfo{}
	}
	if synonyms == nil {
		synonyms = []DrugInfo{}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"analogs":  analogs,
		"synonyms": synonyms,
		"analogs_count":  len(analogs),
		"synonyms_count": len(synonyms),
	})
}

// handleGetInstruction возвращает инструкцию по применению по GUID
func (s *Server) handleGetInstruction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	instructionGUID := r.URL.Query().Get("guid")
	if instructionGUID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан GUID инструкции")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	query := `
		SELECT 
			CAST(GUID_INSTRUCTION AS TEXT) AS GUID_INSTRUCTION,
			ID_INSTRUCTION,
			INSTRUCTION,
			COMPOSITION,
			INDICATION,
			DOSAGE,
			LACTATION,
			PHARM_ACTION,
			CONTRA_INDICATION,
			SIDE_EFFECT,
			INTERACTION,
			OVER_DOSAGE,
			SPECIAL_INSTRUCTION,
			GOODS_DESC,
			STORING_CONDITION
		FROM ES_INSTRUCTION
		WHERE GUID_INSTRUCTION = CAST(@guid AS UUID)
		  AND (DELETED IS NULL OR DELETED = '1900-01-01')
	`

	var instruction Instruction
	var idInstruction sql.NullInt64
	var instructionText, composition, indication, dosage, lactation sql.NullString
	var pharmAction, contraIndication, sideEffect, interaction sql.NullString
	var overDosage, specialInstruction, goodsDesc, storingCondition sql.NullString

	err := s.database.GORMWith(ctx).Raw(query, sql.Named("guid", instructionGUID)).Row().Scan(
		&instruction.GUIDInstruction,
		&idInstruction,
		&instructionText,
		&composition,
		&indication,
		&dosage,
		&lactation,
		&pharmAction,
		&contraIndication,
		&sideEffect,
		&interaction,
		&overDosage,
		&specialInstruction,
		&goodsDesc,
		&storingCondition,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			s.writeError(w, http.StatusNotFound, "Инструкция не найдена")
			return
		}
		if s.logger != nil {
			s.logger.Warn("Ошибка получения инструкции: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения инструкции: %v", err))
		return
	}

	if idInstruction.Valid {
		instruction.IDInstruction = &idInstruction.Int64
	}
	if instructionText.Valid {
		instruction.Instruction = &instructionText.String
	}
	if composition.Valid {
		instruction.Composition = &composition.String
	}
	if indication.Valid {
		instruction.Indication = &indication.String
	}
	if dosage.Valid {
		instruction.Dosage = &dosage.String
	}
	if lactation.Valid {
		instruction.Lactation = &lactation.String
	}
	if pharmAction.Valid {
		instruction.PharmAction = &pharmAction.String
	}
	if contraIndication.Valid {
		instruction.ContraIndication = &contraIndication.String
	}
	if sideEffect.Valid {
		instruction.SideEffect = &sideEffect.String
	}
	if interaction.Valid {
		instruction.Interaction = &interaction.String
	}
	if overDosage.Valid {
		instruction.OverDosage = &overDosage.String
	}
	if specialInstruction.Valid {
		instruction.SpecialInstruction = &specialInstruction.String
	}
	if goodsDesc.Valid {
		instruction.GoodsDesc = &goodsDesc.String
	}
	if storingCondition.Valid {
		instruction.StoringCondition = &storingCondition.String
	}

	s.writeJSON(w, http.StatusOK, instruction)
}

