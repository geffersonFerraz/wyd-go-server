package admin

import (
	"context"
	"sort"

	"dbsrv/internal/store"
)

// effectLabels are the readable names of the item effects. Most are the
// client's own tooltip texts (strdef via SGridControl::m_szParamString and
// dwEFParam); the rest come from Common/ItemEffect.h and from how the Go
// server uses the effect (the ids above 111 that only exist here).
var effectLabels = map[int]string{
	1: "Level necessário", 2: "Aumento de Dano", 3: "Defesa", 4: "Aumento de HP máximo", 5: "Aumento de MP máximo",
	6: "Experiência", 7: "Força", 8: "Inteligência", 9: "Destreza", 10: "Constituição",
	11: "Aprender Arma", 12: "Confiança/Magia Branca/Elemental/Sobrevivência",
	13: "Trans/Magia Negra/Evocação/Troca", 14: "Espada Mágica/Magia Especial/Natureza/Captura",
	15: "Atributo 14 (sem uso)", 16: "Atributo 15 (sem uso)", 17: "Posição de equipamento", 18: "Classe",
	19: "Requisitos com uma mão", 20: "Requisitos com duas mãos", 21: "Tipo de arma",
	22: "Força necessária", 23: "Inteligência necessária", 24: "Destreza necessária", 25: "Constituição necessária",
	26: "Aumento da Velocidade de Ataque", 27: "Alcance", 28: "Cidadania", 29: "Aumento da Velocidade de Movimento",
	30: "Magia ao usar", 31: "Duração", 32: "Parâmetro 2", 33: "Tamanho no inventário", 34: "Tamanho no chão",
	35: "Raça", 36: "Gold (parte alta)", 37: "Gold (parte baixa)", 38: "Consumível", 39: "Número da chave",
	40: "Índice de Evasão", 41: "Índice de Acerto", 42: "Crítico", 43: "Refino", 44: "Economia de Mana",
	45: "Índice de aumento de HP máximo", 46: "Índice de aumento de MP máximo",
	47: "Índice de regeneração de HP", 48: "Índice de regeneração de MP",
	49: "Resistência a Fogo", 50: "Resistência a Gelo", 51: "Resistência a Sagrado", 52: "Resistência a Relâmpago",
	53: "Defesa (adicional)", 54: "Aumento de Imunidades", 55: "Bônus",
	56: "Guilda: ataque PvP", 57: "Guilda: defesa PvP", 58: "Missão", 59: "Tipo exclusivo", 60: "Ataque Mágico",
	61: "Quantidade", 62: "Regeneração de HP bônus", 63: "Número (parte baixa)",
	64: "Reiniciar Confiança/Magia Branca/Elemental", 65: "Reiniciar Trans/Magia Negra/Evocação",
	66: "Reiniciar Espada Mágica/Magia Especial/Natureza",
	67: "Aumento de Dano (adicional)", 68: "Ataque Mágico (adicional)",
	69: "Aumento de HP máximo (base)", 70: "Aumento de MP máximo (base)", 71: "Crítico (base)",
	72: "Defesa (base)", 73: "Aumento de Dano (base)", 74: "Aumento de Aprendizagem de Skill",
	75: "Kills atuais", 76: "Total de kills (parte baixa)", 77: "Total de kills (parte alta)",
	78: "Valor Crítico de Incubação", 79: "Vitalidade da montaria", 80: "HP da montaria",
	81: "Crescimento da montaria", 82: "Ração da montaria", 83: "EX da montaria",
	84: "Tempo de Espera para Incubação", 85: "Subguilda", 86: "Aparência de mob", 87: "Nível de refino",
	88: "Sala de jogos", 89: "Dano absoluto", 90: "Defesa absoluta", 91: "Donate", 92: "Honra",
	100: "Grau 0", 101: "Grau 1", 102: "Grau 2", 103: "Grau 3", 104: "Grau 4", 105: "Grau 5",
	106: "Data: dia", 107: "Data: hora", 108: "Data: minuto", 109: "Data: ano", 110: "Data: mês",
	111: "Intransferível", 112: "Evolução exigida", 113: "Fama", 115: "Selado",
	116: "Cor 0 / Tipo do item", 117: "Cor 1 / Sem refino", 118: "Cor 2", 119: "Cor 3", 120: "Cor 4",
	121: "Cor 5", 122: "Cor 6", 123: "Cor 7", 124: "Cor 8", 125: "Cor 9",
	150: "Roubo de vida", 151: "Bônus de experiência", 152: "Bônus de cura de poção",
	153: "Bônus de experiência individual", 154: "Vampirismo", 155: "Ignorar resistência",
	156: "Chance de lentidão", 157: "Chance de resistir", 200: "Intransferível",
	201: "Presente único por conta", 202: "Bônus de drop", 203: "Lista de prêmios",
}

// effectNames is the effect list shown in the panel: every id the database
// knows or the panel has a label for, with the readable name.
func (s *Server) effectNames(ctx context.Context) ([]store.EffectName, error) {
	list, err := s.st.EffectNameList(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[int]bool, len(list))
	for i := range list {
		seen[list[i].ID] = true
		if l := effectLabels[list[i].ID]; l != "" {
			list[i].Name = l
		}
	}
	for id, l := range effectLabels {
		if !seen[id] {
			list = append(list, store.EffectName{ID: id, Name: l})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list, nil
}
