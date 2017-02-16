/*
 * Copyright 2017 Manuel Gauto (github.com/twa16)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
*/

package main

import (
	"crypto/rsa"
	"crypto/tls"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/acme/autocert"

	"github.com/jinzhu/gorm"
	"github.com/spf13/viper"
	"errors"
	"os"
)

//NginxInstance Represents the nginx instance.
type NginxInstance struct {
	SitesDirectory string //Path to the sites-enabled directory
	ReloadCommand  string //Command to execute when attempting to reload Nginx
}

//NginxProxyConfiguration contains the data that is used to configure a reverse proxy on Nginx
type NginxProxyConfiguration struct {
	gorm.Model
	DomainName      string //Optional. Generated if not provided. Regenerated if not unique.
	IsHTTPS         bool   //Required
	CertificatePath string
	PrivateKeyPath  string
	Destination     string //Required
	DeploymentID    uint   //Required
	ConfigurationFilePath string
}

//ApplyChanges Called when mds wishes to reload Nginx
func (n *NginxInstance) ApplyChanges() error {
	log.Warning("Reloading Nginx")
	commandParts := viper.GetStringSlice("NginxReloadCommand")
	cmd := exec.Command(commandParts[0], commandParts[1:len(commandParts)]...)
	_, err := cmd.Output()
	return err
}

//CreateProxy creates a proxy configuration in Nginx
func (n *NginxInstance) CreateProxy(db *gorm.DB, config *NginxProxyConfiguration) (string, error) {
	log.Infof("Creating Proxy for deployment %d", config.ID)
	//Let's grab the template
	var templateFileName string
	if config.IsHTTPS {
		templateFileName = viper.GetString("DataDirectory") + "/https-site-nginx.template"
	} else {
		templateFileName = viper.GetString("DataDirectory") + "/http-site-nginx.template"
	}
	templateBytes, err := ioutil.ReadFile(templateFileName)
	if err != nil {
		return "", err
	}
	//Convert the bytes to a string
	templateString := string(templateBytes)

	//Build domain name
	//domainName := config.domainName
	var domainName = config.DomainName

	//Set the values in the configuration
	configString := strings.Replace(templateString, "{{domainName}}", domainName, -1)
	configString = strings.Replace(configString, "{{destination}}", config.Destination, -1)

	//Set HTTPS options if necessary
	if config.IsHTTPS {
		configString = strings.Replace(configString, "{{certificatePath}}", config.CertificatePath, -1)
		configString = strings.Replace(configString, "{{privateKeyPath}}", config.PrivateKeyPath, -1)
		var privateKey *rsa.PrivateKey
		var certificate []byte

		//Check which way to get certs
		if viper.GetString("CertProvider") == "selfsigned" {
			log.Infof("Generating RSA Keys with 'selfsigned' provider\n")
			privateKey, certificate, err = CreateSelfSignedCertificate(domainName)
			if err != nil {
				return "", err
			}
			log.Info("Persisting generated key material")
			err = WriteCertificateToFile(certificate, config.CertificatePath)
			if err != nil {
				return "", err
			}
			err = WritePrivateKeyToFile(privateKey, config.PrivateKeyPath)
			if err != nil {
				return "", err
			}
		}
		//TODO: handle use of letsencrypt
	}

	//Write the template to a file to the sites-available directory
	var fileName = n.SitesDirectory + "MDS-" + domainName + ".conf"
	//Cleanup old file if it exists
	os.Remove(fileName)
	//Save the path to the config
	config.ConfigurationFilePath = fileName
	err = ioutil.WriteFile(fileName, []byte(configString), 0644)
	if err != nil {
		return "", err
	}

	//Reload Nginx
	err = n.ApplyChanges()
	if err != nil {
		log.Criticalf("Failed to apply changes to Nginx")
		return "", err
	}

	return domainName, nil
}

func (n *NginxInstance) DeleteProxyConfiguration(db *gorm.DB, domainName string) error {
	//Let's get the details about this proxy
	nginxConfig := NginxProxyConfiguration{}
	resp := db.Where("DomainName = ?", domainName).First(nginxConfig)
	//Throw an error if the configuration was not found
	if resp.RecordNotFound() {
		return errors.New("No proxy with that domain name was found")
	}
	//Remove the config file
	os.Remove(nginxConfig.ConfigurationFilePath)
	//Remove the key material if the site is https
	if nginxConfig.IsHTTPS {
		os.Remove(nginxConfig.CertificatePath)
		os.Remove(nginxConfig.PrivateKeyPath)
	}
	//Finally, remove the config itself
	db.Delete(&nginxConfig)
	return nil
}

func (n *NginxInstance) GenerateHTTPSSettings(config NginxProxyConfiguration) NginxProxyConfiguration {
	certPath, err := filepath.Abs(viper.GetString("CertDestination"))
	if err != nil {
		log.Fatal("Failed to expand certificate path: "+viper.GetString("CertDestination"))
	}
	certificatePath := certPath + string(filepath.Separator) + config.DomainName + ".cer"
	privateKeyPath := certPath + string(filepath.Separator) + config.DomainName + ".key"

	config.CertificatePath = certificatePath
	config.PrivateKeyPath = privateKeyPath
	config.IsHTTPS = true
	return config
}

//ReserveDomainName Reserves a domain name in the DB by creating an unaffiliated NginxConfig
func ReserveDomainName(db *gorm.DB) NginxProxyConfiguration {
	nginxConfig := NginxProxyConfiguration{}
	nginxConfig.DomainName = GenerateNewUniqueURL(db)
	db.Create(&nginxConfig)
	return nginxConfig
}

//GenerateNewUniqueURL Generates a new URL to be used by an application
func GenerateNewUniqueURL(db *gorm.DB) string {
	//Loop until a unique hostname is generated
	for true {
		domainName := GenerateCodename() + viper.GetString("UrlBase")
		log.Debugf("Generated Domain Name: %s", domainName)
		if IsDomainNameUnique(db, domainName) {
			return domainName
		}
	}
	//This should never happen
	return "ERROR"
}

//IsDomainNameUnique checks the database to see if the domain name is unique
func IsDomainNameUnique(db *gorm.DB, domainName string) bool {
	log.Debugf("Result of unqiue check for %s is %b\n", domainName, db.Where(&NginxProxyConfiguration{DomainName: domainName}).RecordNotFound())
	return db.Where(&NginxProxyConfiguration{DomainName: domainName}).First(&NginxProxyConfiguration{}).RecordNotFound()
}

//TODO: Finish Implementation of this
func autocertSetup() {
	m := autocert.Manager{
		Prompt: autocert.AcceptTOS,
	}
	s := &http.Server{
		Addr:      ":https",
		TLSConfig: &tls.Config{GetCertificate: m.GetCertificate},
	}
	s.ListenAndServeTLS("", "")
}

//GenerateCodename generates a twoword domain name that is built using the word list
func GenerateCodename() string {
	codename := ""
	for i := 0; i < 2; i++ {
		randInt := rand.Intn(len(mnemonicWords))
		codename += mnemonicWords[randInt]
	}
	return codename
}

var mnemonicWords = []string{
	"academy", "acrobat", "active", "actor", "adam", "admiral",
	"adrian", "africa", "agenda", "agent", "airline", "airport",
	"aladdin", "alarm", "alaska", "albert", "albino", "album",
	"alcohol", "alex", "algebra", "alibi", "alice", "alien",
	"alpha", "alpine", "amadeus", "amanda", "amazon", "amber",
	"america", "amigo", "analog", "anatomy", "angel", "animal",
	"antenna", "antonio", "apollo", "april", "archive", "arctic",
	"arizona", "arnold", "aroma", "arthur", "artist", "asia",
	"aspect", "aspirin", "athena", "athlete", "atlas", "audio",
	"august", "austria", "axiom", "aztec", "balance", "ballad",
	"banana", "bandit", "banjo", "barcode", "baron", "basic",
	"battery", "belgium", "berlin", "bermuda", "bernard", "bikini",
	"binary", "bingo", "biology", "block", "blonde", "bonus",
	"boris", "boston", "boxer", "brandy", "bravo", "brazil",
	"bronze", "brown", "bruce", "bruno", "burger", "burma",
	"cabinet", "cactus", "cafe", "cairo", "cake", "calypso",
	"camel", "camera", "campus", "canada", "canal", "cannon",
	"canoe", "cantina", "canvas", "canyon", "capital", "caramel",
	"caravan", "carbon", "cargo", "carlo", "carol", "carpet",
	"cartel", "casino", "castle", "castro", "catalog", "caviar",
	"cecilia", "cement", "center", "century", "ceramic", "chamber",
	"chance", "change", "chaos", "charlie", "charm", "charter",
	"chef", "chemist", "cherry", "chess", "chicago", "chicken",
	"chief", "china", "cigar", "cinema", "circus", "citizen",
	"city", "clara", "classic", "claudia", "clean", "client",
	"climax", "clinic", "clock", "club", "cobra", "coconut",
	"cola", "collect", "colombo", "colony", "color", "combat",
	"comedy", "comet", "command", "compact", "company", "complex",
	"concept", "concert", "connect", "consul", "contact", "context",
	"contour", "control", "convert", "copy", "corner", "corona",
	"correct", "cosmos", "couple", "courage", "cowboy", "craft",
	"crash", "credit", "cricket", "critic", "crown", "crystal",
	"cuba", "culture", "dallas", "dance", "daniel", "david",
	"decade", "decimal", "deliver", "delta", "deluxe", "demand",
	"demo", "denmark", "derby", "design", "detect", "develop",
	"diagram", "dialog", "diamond", "diana", "diego", "diesel",
	"diet", "digital", "dilemma", "diploma", "direct", "disco",
	"disney", "distant", "doctor", "dollar", "dominic", "domino",
	"donald", "dragon", "drama", "dublin", "duet", "dynamic",
	"east", "ecology", "economy", "edgar", "egypt", "elastic",
	"elegant", "element", "elite", "elvis", "email", "energy",
	"engine", "english", "episode", "equator", "escort", "ethnic",
	"europe", "everest", "evident", "exact", "example", "exit",
	"exotic", "export", "express", "extra", "fabric", "factor",
	"falcon", "family", "fantasy", "fashion", "fiber", "fiction",
	"fidel", "fiesta", "figure", "film", "filter", "final",
	"finance", "finish", "finland", "flash", "florida", "flower",
	"fluid", "flute", "focus", "ford", "forest", "formal",
	"format", "formula", "fortune", "forum", "fragile", "france",
	"frank", "friend", "frozen", "future", "gabriel", "galaxy",
	"gallery", "gamma", "garage", "garden", "garlic", "gemini",
	"general", "genetic", "genius", "germany", "global", "gloria",
	"golf", "gondola", "gong", "good", "gordon", "gorilla",
	"grand", "granite", "graph", "green", "group", "guide",
	"guitar", "guru", "hand", "happy", "harbor", "harmony",
	"harvard", "havana", "hawaii", "helena", "hello", "henry",
	"hilton", "history", "horizon", "hotel", "human", "humor",
	"icon", "idea", "igloo", "igor", "image", "impact",
	"import", "index", "india", "indigo", "input", "insect",
	"instant", "iris", "italian", "jacket", "jacob", "jaguar",
	"janet", "japan", "jargon", "jazz", "jeep", "john",
	"joker", "jordan", "jumbo", "june", "jungle", "junior",
	"jupiter", "karate", "karma", "kayak", "kermit", "kilo",
	"king", "koala", "korea", "labor", "lady", "lagoon",
	"laptop", "laser", "latin", "lava", "lecture", "left",
	"legal", "lemon", "level", "lexicon", "liberal", "libra",
	"limbo", "limit", "linda", "linear", "lion", "liquid",
	"liter", "little", "llama", "lobby", "lobster", "local",
	"logic", "logo", "lola", "london", "lotus", "lucas",
	"lunar", "machine", "macro", "madam", "madonna", "madrid",
	"maestro", "magic", "magnet", "magnum", "major", "mama",
	"mambo", "manager", "mango", "manila", "marco", "marina",
	"market", "mars", "martin", "marvin", "master", "matrix",
	"maximum", "media", "medical", "mega", "melody", "melon",
	"memo", "mental", "mentor", "menu", "mercury", "message",
	"metal", "meteor", "meter", "method", "metro", "mexico",
	"miami", "micro", "million", "mineral", "minimum", "minus",
	"minute", "miracle", "mirage", "miranda", "mister", "mixer",
	"mobile", "model", "modem", "modern", "modular", "moment",
	"monaco", "monica", "monitor", "mono", "monster", "montana",
	"morgan", "motel", "motif", "motor", "mozart", "multi",
	"museum", "music", "mustang", "natural", "neon", "nepal",
	"neptune", "nerve", "neutral", "nevada", "news", "ninja",
	"nirvana", "normal", "nova", "novel", "nuclear", "numeric",
	"nylon", "oasis", "object", "observe", "ocean", "octopus",
	"olivia", "olympic", "omega", "opera", "optic", "optimal",
	"orange", "orbit", "organic", "orient", "origin", "orlando",
	"oscar", "oxford", "oxygen", "ozone", "pablo", "pacific",
	"pagoda", "palace", "pamela", "panama", "panda", "panel",
	"panic", "paradox", "pardon", "paris", "parker", "parking",
	"parody", "partner", "passage", "passive", "pasta", "pastel",
	"patent", "patriot", "patrol", "patron", "pegasus", "pelican",
	"penguin", "pepper", "percent", "perfect", "perfume", "period",
	"permit", "person", "peru", "phone", "photo", "piano",
	"picasso", "picnic", "picture", "pigment", "pilgrim", "pilot",
	"pirate", "pixel", "pizza", "planet", "plasma", "plaster",
	"plastic", "plaza", "pocket", "poem", "poetic", "poker",
	"polaris", "police", "politic", "polo", "polygon", "pony",
	"popcorn", "popular", "postage", "postal", "precise", "prefix",
	"premium", "present", "price", "prince", "printer", "prism",
	"private", "product", "profile", "program", "project", "protect",
	"proton", "public", "pulse", "puma", "pyramid", "queen",
	"radar", "radio", "random", "rapid", "rebel", "record",
	"recycle", "reflex", "reform", "regard", "regular", "relax",
	"report", "reptile", "reverse", "ricardo", "ringo", "ritual",
	"robert", "robot", "rocket", "rodeo", "romeo", "royal",
	"russian", "safari", "salad", "salami", "salmon", "salon",
	"salute", "samba", "sandra", "santana", "sardine", "school",
	"screen", "script", "second", "secret", "section", "segment",
	"select", "seminar", "senator", "senior", "sensor", "serial",
	"service", "sheriff", "shock", "sierra", "signal", "silicon",
	"silver", "similar", "simon", "single", "siren", "slogan",
	"social", "soda", "solar", "solid", "solo", "sonic",
	"soviet", "special", "speed", "spiral", "spirit", "sport",
	"static", "station", "status", "stereo", "stone", "stop",
	"street", "strong", "student", "studio", "style", "subject",
	"sultan", "super", "susan", "sushi", "suzuki", "switch",
	"symbol", "system", "tactic", "tahiti", "talent", "tango",
	"tarzan", "taxi", "telex", "tempo", "tennis", "texas",
	"textile", "theory", "thermos", "tiger", "titanic", "tokyo",
	"tomato", "topic", "tornado", "toronto", "torpedo", "total",
	"totem", "tourist", "tractor", "traffic", "transit", "trapeze",
	"travel", "tribal", "trick", "trident", "trilogy", "tripod",
	"tropic", "trumpet", "tulip", "tuna", "turbo", "twist",
	"ultra", "uniform", "union", "uranium", "vacuum", "valid",
	"vampire", "vanilla", "vatican", "velvet", "ventura", "venus",
	"vertigo", "veteran", "victor", "video", "vienna", "viking",
	"village", "vincent", "violet", "violin", "virtual", "virus",
	"visa", "vision", "visitor", "visual", "vitamin", "viva",
	"vocal", "vodka", "volcano", "voltage", "volume", "voyage",
	"water", "weekend", "welcome", "western", "window", "winter",
	"wizard", "wolf", "world", "xray", "yankee", "yoga",
	"yogurt", "yoyo", "zebra", "zero", "zigzag", "zipper",
	"zodiac", "zoom", "abraham", "action", "address", "alabama",
	"alfred", "almond", "ammonia", "analyze", "annual", "answer",
	"apple", "arena", "armada", "arsenal", "atlanta", "atomic",
	"avenue", "average", "bagel", "baker", "ballet", "bambino",
	"bamboo", "barbara", "basket", "bazaar", "benefit", "bicycle",
	"bishop", "blitz", "bonjour", "bottle", "bridge", "british",
	"brother", "brush", "budget", "cabaret", "cadet", "candle",
	"capitan", "capsule", "career", "cartoon", "channel", "chapter",
	"cheese", "circle", "cobalt", "cockpit", "college", "compass",
	"comrade", "condor", "crimson", "cyclone", "darwin", "declare",
	"degree", "delete", "delphi", "denver", "desert", "divide",
	"dolby", "domain", "domingo", "double", "drink", "driver",
	"eagle", "earth", "echo", "eclipse", "editor", "educate",
	"edward", "effect", "electra", "emerald", "emotion", "empire",
	"empty", "escape", "eternal", "evening", "exhibit", "expand",
	"explore", "extreme", "ferrari", "first", "flag", "folio",
	"forget", "forward", "freedom", "fresh", "friday", "fuji",
	"galileo", "garcia", "genesis", "gold", "gravity", "habitat",
	"hamlet", "harlem", "helium", "holiday", "house", "hunter",
	"ibiza", "iceberg", "imagine", "infant", "isotope", "jackson",
	"jamaica", "jasmine", "java", "jessica", "judo", "kitchen",
	"lazarus", "letter", "license", "lithium", "loyal", "lucky",
	"magenta", "mailbox", "manual", "marble", "mary", "maxwell",
	"mayor", "milk", "monarch", "monday", "money", "morning",
	"mother", "mystery", "native", "nectar", "nelson", "network",
	"next", "nikita", "nobel", "nobody", "nominal", "norway",
	"nothing", "number", "october", "office", "oliver", "opinion",
	"option", "order", "outside", "package", "pancake", "pandora",
	"panther", "papa", "patient", "pattern", "pedro", "pencil",
	"people", "phantom", "philips", "pioneer", "pluto", "podium",
	"portal", "potato", "prize", "process", "protein", "proxy",
	"pump", "pupil", "python", "quality", "quarter", "quiet",
	"rabbit", "radical", "radius", "rainbow", "ralph", "ramirez",
	"ravioli", "raymond", "respect", "respond", "result", "resume",
	"retro", "richard", "right", "risk", "river", "roger",
	"roman", "rondo", "sabrina", "salary", "salsa", "sample",
	"samuel", "saturn", "savage", "scarlet", "scoop", "scorpio",
	"scratch", "scroll", "sector", "serpent", "shadow", "shampoo",
	"sharon", "sharp", "short", "shrink", "silence", "silk",
	"simple", "slang", "smart", "smoke", "snake", "society",
	"sonar", "sonata", "soprano", "source", "sparta", "sphere",
	"spider", "sponsor", "spring", "acid", "adios", "agatha",
	"alamo", "alert", "almanac", "aloha", "andrea", "anita",
	"arcade", "aurora", "avalon", "baby", "baggage", "balloon",
	"bank", "basil", "begin", "biscuit", "blue", "bombay",
	"brain", "brenda", "brigade", "cable", "carmen", "cello",
	"celtic", "chariot", "chrome", "citrus", "civil", "cloud",
	"common", "compare", "cool", "copper", "coral", "crater",
	"cubic", "cupid", "cycle", "depend", "door", "dream",
	"dynasty", "edison", "edition", "enigma", "equal", "eric",
	"event", "evita", "exodus", "extend", "famous", "farmer",
	"food", "fossil", "frog", "fruit", "geneva", "gentle",
	"george", "giant", "gilbert", "gossip", "gram", "greek",
	"grille", "hammer", "harvest", "hazard", "heaven", "herbert",
	"heroic", "hexagon", "husband", "immune", "inca", "inch",
	"initial", "isabel", "ivory", "jason", "jerome", "joel",
	"joshua", "journal", "judge", "juliet", "jump", "justice",
	"kimono", "kinetic", "leonid", "lima", "maze", "medusa",
	"member", "memphis", "michael", "miguel", "milan", "mile",
	"miller", "mimic", "mimosa", "mission", "monkey", "moral",
	"moses", "mouse", "nancy", "natasha", "nebula", "nickel",
	"nina", "noise", "orchid", "oregano", "origami", "orinoco",
	"orion", "othello", "paper", "paprika", "prelude", "prepare",
	"pretend", "profit", "promise", "provide", "puzzle", "remote",
	"repair", "reply", "rival", "riviera", "robin", "rose",
	"rover", "rudolf", "saga", "sahara", "scholar", "shelter",
	"ship", "shoe", "sigma", "sister", "sleep", "smile",
	"spain", "spark", "split", "spray", "square", "stadium",
	"star", "storm", "story", "strange", "stretch", "stuart",
	"subway", "sugar", "sulfur", "summer", "survive", "sweet",
	"swim", "table", "taboo", "target", "teacher", "telecom",
	"temple", "tibet", "ticket", "tina", "today", "toga",
	"tommy", "tower", "trivial", "tunnel", "turtle", "twin",
	"uncle", "unicorn", "unique", "update", "valery", "vega",
	"version", "voodoo", "warning", "william", "wonder", "year",
	"yellow", "young", "absent", "absorb", "accent", "alfonso",
	"alias", "ambient", "andy", "anvil", "appear", "apropos",
	"archer", "ariel", "armor", "arrow", "austin", "avatar",
	"axis", "baboon", "bahama", "bali", "balsa", "bazooka",
	"beach", "beast", "beatles", "beauty", "before", "benny",
	"betty", "between", "beyond", "billy", "bison", "blast",
	"bless", "bogart", "bonanza", "book", "border", "brave",
	"bread", "break", "broken", "bucket", "buenos", "buffalo",
	"bundle", "button", "buzzer", "byte", "caesar", "camilla",
	"canary", "candid", "carrot", "cave", "chant", "child",
	"choice", "chris", "cipher", "clarion", "clark", "clever",
	"cliff", "clone", "conan", "conduct", "congo", "content",
	"costume", "cotton", "cover", "crack", "current", "danube",
	"data", "decide", "desire", "detail", "dexter", "dinner",
	"dispute", "donor", "druid", "drum", "easy", "eddie",
	"enjoy", "enrico", "epoxy", "erosion", "except", "exile",
	"explain", "fame", "fast", "father", "felix", "field",
	"fiona", "fire", "fish", "flame", "flex", "flipper",
	"float", "flood", "floor", "forbid", "forever", "fractal",
	"frame", "freddie", "front", "fuel", "gallop", "game",
	"garbo", "gate", "gibson", "ginger", "giraffe", "gizmo",
	"glass", "goblin", "gopher", "grace", "gray", "gregory",
	"grid", "griffin", "ground", "guest", "gustav", "gyro",
	"hair", "halt", "harris", "heart", "heavy", "herman",
	"hippie", "hobby", "honey", "hope", "horse", "hostel",
	"hydro", "imitate", "info", "ingrid", "inside", "invent",
	"invest", "invite", "iron", "ivan", "james", "jester",
	"jimmy", "join", "joseph", "juice", "julius", "july",
	"justin", "kansas", "karl", "kevin", "kiwi", "ladder",
	"lake", "laura", "learn", "legacy", "legend", "lesson",
	"life", "light", "list", "locate", "lopez", "lorenzo",
	"love", "lunch", "malta", "mammal", "margo", "marion",
	"mask", "match", "mayday", "meaning", "mercy", "middle",
	"mike", "mirror", "modest", "morph", "morris", "nadia",
	"nato", "navy", "needle", "neuron", "never", "newton",
	"nice", "night", "nissan", "nitro", "nixon", "north",
	"oberon", "octavia", "ohio", "olga", "open", "opus",
	"orca", "oval", "owner", "page", "paint", "palma",
	"parade", "parent", "parole", "paul", "peace", "pearl",
	"perform", "phoenix", "phrase", "pierre", "pinball", "place",
	"plate", "plato", "plume", "pogo", "point", "polite",
	"polka", "poncho", "powder", "prague", "press", "presto",
	"pretty", "prime", "promo", "quasi", "quest", "quick",
	"quiz", "quota", "race", "rachel", "raja", "ranger",
	"region", "remark", "rent", "reward", "rhino", "ribbon",
	"rider", "road", "rodent", "round", "rubber", "ruby",
	"rufus", "sabine", "saddle", "sailor", "saint", "salt",
	"satire", "scale", "scuba", "season", "secure", "shake",
	"shallow", "shannon", "shave", "shelf", "sherman", "shine",
	"shirt", "side", "sinatra", "sincere", "size", "slalom",
	"slow", "small", "snow", "sofia", "song", "sound",
	"south", "speech", "spell", "spend", "spoon", "stage",
	"stamp", "stand", "state", "stella", "stick", "sting",
	"stock", "store", "sunday", "sunset", "support", "sweden",
	"swing", "tape", "think", "thomas", "tictac", "time",
	"toast", "tobacco", "tonight", "torch", "torso", "touch",
	"toyota", "trade", "tribune", "trinity", "triton", "truck",
	"trust", "type", "under", "unit", "urban", "urgent",
	"user", "value", "vendor", "venice", "verona", "vibrate",
	"virgo", "visible", "vista", "vital", "voice", "vortex",
	"waiter", "watch", "wave", "weather", "wedding", "wheel",
	"whiskey", "wisdom", "deal", "null", "nurse", "quebec",
	"reserve", "reunion", "roof", "singer", "verbal", "amen",
	"ego", "fax", "jet", "job", "rio", "ski",
	"yes",
}
