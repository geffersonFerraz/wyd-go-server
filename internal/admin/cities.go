package admin

import (
	"context"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"dbsrv/internal/store"
)

// City ownership: which guild holds each city (guild_zone.txt rows 0..4).
// The owner's members are recalled to the guild area, pay no recall back to
// the city, get the city's taxes and so on. Saved in gamedata.city_owners;
// the running TMSRVs pick it up within a second.

var cityNames = [store.MaxCity]string{"Armia", "Azran", "Erion", "Nippleheim", "Noatun"}

type cityRow struct {
	ID        int
	Name      string
	GuildID   int
	GuildName string
	Set       bool // set in the panel (otherwise guild_zone.txt decides)
	ChangedBy string
	ChangedAt string
}

// channels are the channels the panel offers.
func (s *Server) channels() []int {
	if len(s.cfg.Channels) == 0 {
		return []int{1}
	}
	return s.cfg.Channels
}

// pickChannel reads ?channel= or the form's channel (default: the first).
func (s *Server) pickChannel(r *http.Request) int {
	list := s.channels()
	v, _ := strconv.Atoi(r.FormValue("channel"))
	if slices.Contains(list, v) {
		return v
	}
	return list[0]
}

func (s *Server) cityRows(r *http.Request, guilds []store.Guild, channel int) ([]cityRow, error) {
	owners, err := s.st.CityOwners(r.Context())
	if err != nil {
		return nil, err
	}
	names := map[int]string{}
	for _, g := range guilds {
		names[g.ID] = g.Name
	}
	rows := make([]cityRow, store.MaxCity)
	for i := range rows {
		rows[i] = cityRow{ID: i, Name: cityNames[i]}
	}
	for _, o := range owners {
		if o.Channel != channel || o.City < 0 || o.City >= store.MaxCity {
			continue
		}
		c := &rows[o.City]
		c.GuildID, c.GuildName, c.Set = o.GuildID, names[o.GuildID], true
		c.ChangedBy, c.ChangedAt = o.ChangedBy, o.ChangedAt.Local().Format("02/01/2006 15:04")
	}
	return rows, nil
}

// SeedCityOwners copies the owners of TMSRV's guild_zone.txt into
// gamedata.city_owners for the cities that have no row yet, so the database
// is the only place city owners are kept (the panel manages them; the GM
// commands +guildowner / +resetcities are gone). Run at panel startup.
func SeedCityOwners(ctx context.Context, st *store.Store, guildZone string) (int, error) {
	raw, err := os.ReadFile(guildZone)
	if err != nil {
		return 0, err
	}
	owners := map[int]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Split(strings.TrimSpace(line), ",")
		if len(f) < 2 {
			continue
		}
		city, err1 := strconv.Atoi(strings.TrimSpace(f[0]))
		guild, err2 := strconv.Atoi(strings.TrimSpace(f[1]))
		if err1 == nil && err2 == nil {
			owners[city] = guild
		}
	}
	return st.SeedCityOwners(ctx, owners, "guild_zone.txt")
}

// resetCities is POST /cities/reset: every city of a channel without owner.
func (s *Server) resetCities(w http.ResponseWriter, r *http.Request) {
	by := sessionOf(r).user
	channel := s.pickChannel(r)
	for c := range store.MaxCity {
		if err := s.st.SetCityOwner(r.Context(), channel, c, 0, by); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.log.Info("cities reset", "admin", by, "channel", channel)
	s.renderGuilds(w, r, http.StatusOK, "Todas as cidades do canal "+strconv.Itoa(channel)+" ficaram sem dono. Vale em até alguns segundos.", "")
}

// setCityOwner is POST /cities/{city} with guild (id or exact name; 0 or
// empty = no owner).
func (s *Server) setCityOwner(w http.ResponseWriter, r *http.Request) {
	city, err := strconv.Atoi(r.PathValue("city"))
	if err != nil || city < 0 || city >= store.MaxCity {
		http.NotFound(w, r)
		return
	}
	guilds, err := s.st.Guilds(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	in := strings.TrimSpace(r.PostFormValue("guild"))
	guildID := -1
	if in == "" || in == "0" {
		guildID = 0
	} else if id, err := strconv.Atoi(in); err == nil {
		for _, g := range guilds {
			if g.ID == id {
				guildID = id
			}
		}
	} else {
		for _, g := range guilds {
			if strings.EqualFold(g.Name, in) {
				guildID = g.ID
			}
		}
	}
	if guildID < 0 {
		s.renderGuilds(w, r, http.StatusUnprocessableEntity, "", "Guilda \""+in+"\" não encontrada (use o id ou o nome exato).")
		return
	}
	by := sessionOf(r).user
	channel := s.pickChannel(r)
	if err := s.st.SetCityOwner(r.Context(), channel, city, guildID, by); err != nil {
		s.fail(w, err)
		return
	}
	s.log.Info("city owner set", "admin", by, "channel", channel, "city", cityNames[city], "guild", guildID)
	msg := cityNames[city] + " agora não tem dono."
	if guildID != 0 {
		for _, g := range guilds {
			if g.ID == guildID {
				msg = cityNames[city] + " agora pertence à guilda " + g.Name + " (#" + strconv.Itoa(g.ID) + ")."
			}
		}
	}
	s.renderGuilds(w, r, http.StatusOK, "Canal "+strconv.Itoa(channel)+": "+msg+" Vale em até alguns segundos.", "")
}
