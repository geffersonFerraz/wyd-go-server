package game

import "dbsrv/internal/exptable"

// nextLevel is g_pNextLevel[evolution][level]: total exp needed for level+1
// (shared with DBSRV and the admin panel).
var nextLevel = &exptable.Next
