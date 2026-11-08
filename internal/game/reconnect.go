package game

import (
	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// opReconnectPos is MSG_Reconect (0xAF5): after a reconnect the client says
// where it was; inside an event room the player goes back to town.
const opReconnectPos = 0xAF5

type posArea struct{ minX, minY, maxX, maxY int }

func (a posArea) has(x, y int) bool { return x >= a.minX && x <= a.maxX && y >= a.minY && y <= a.maxY }

// reconnectAreas are the rooms a reconnect cannot resume: the Barlog
// teleports (g_pTeleBarlog), the Pista de Runas (g_pPistaCoords) and the
// water rooms (waterMaxMin). The C++ compared the first two with the columns
// in the wrong order, so they never matched.
var reconnectAreas = []posArea{
	{3379, 1166, 3381, 1168}, {3379, 1178, 3381, 1180},
	{3340, 1223, 3342, 1225}, {3351, 1223, 3353, 1226}, {3362, 1223, 3364, 1225},
	{3378, 1237, 3380, 1239}, {3386, 1237, 3388, 1239}, {3394, 1237, 3396, 1239}, {3402, 1237, 3404, 1239},
	{3429, 1197, 3431, 1199},

	{3328, 1599, 3452, 1660}, {3328, 1539, 3452, 1595}, {3328, 1407, 3452, 1466}, {3328, 1025, 3452, 1148},
	{3328, 1280, 3454, 1406}, {3328, 1153, 3452, 1276}, {3328, 1467, 3452, 1535},

	{1040, 3545, 1055, 3560}, {1040, 3510, 1055, 3525}, {1040, 3475, 1055, 3490}, {1080, 3475, 1095, 3490}, {1115, 3475, 1130, 3490},
	{1115, 3510, 1130, 3525}, {1115, 3545, 1130, 3560}, {1075, 3545, 1096, 3560}, {1075, 3505, 1095, 3530},
	{1170, 3675, 1185, 3690}, {1170, 3635, 1185, 3655}, {1170, 3600, 1185, 3620}, {1205, 3600, 1220, 3620}, {1240, 3600, 1255, 3620},
	{1240, 3640, 1255, 3655}, {1240, 3675, 1255, 3690}, {1205, 3675, 1220, 3690}, {1205, 3630, 1225, 3655},
	// Quests. The C++ read these columns as x1, x2, y1, y2, so most never
	// matched and the Kaizen one matched almost the whole map.
	{3600, 3600, 3700, 3700}, {3713, 3459, 3838, 3582}, {3840, 3584, 3967, 3712}, // Lan House N, M, A
	{2380, 2078, 2425, 2133}, // Coveiro
	{2229, 1701, 2256, 1728}, // Jardim
	{459, 3886, 500, 3917},   // Kaizen
	{657, 3728, 704, 3764},   // Hidra
	{1308, 4023, 1350, 4057}, // Emblema da Proteção
	{793, 4046, 828, 4080},   // Molar
	{2545, 1934, 2552, 1957}, // Molar 201-255
	{1714, 1718, 1738, 1745}, // Reino
	{2232, 1564, 2256, 1589}, // Capa verde
	{2639, 1966, 2670, 2004}, // Bênção de Deus
	{1949, 1586, 1989, 1615}, // Equilíbrio da Força
	{301, 407, 330, 487},

	{1295, 3545, 1315, 3565}, {1295, 3510, 1315, 3525}, {1300, 3470, 1315, 3490}, {1335, 3475, 1350, 3490}, {1370, 3475, 1385, 3490},
	{1370, 3510, 1385, 3525}, {1365, 3545, 1385, 3565}, {1330, 3545, 1350, 3565}, {1330, 3505, 1355, 3530},
}

func (g *Game) requestReconnectPos(u *User, pkt protocol.Packet) {
	var p wire.Parm2
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	g.clientMessage(u, "Posição recuperada com sucesso.")
	for _, a := range reconnectAreas {
		if a.has(int(p.Parm1), int(p.Parm2)) {
			g.doRecall(u.id)
			return
		}
	}
}
