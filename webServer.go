package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	_ "github.com/lib/pq"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	scrap "github.com/badoux/goscraper"
	"github.com/likexian/whois-go"
	whoisparser "github.com/ikermatias/whois-parser-go"
)

var wg sync.WaitGroup

/**
Struct tipo Server
*/
type Server struct {
	Address  string `json:"address"`
	SslGrade string `json:"ssl_grade"`
	Country  string `json:"country"`
	Owner    string `json:"owner"`
}

/**
Struct tipo Dominio
*/
type Dominio struct {
	Name             string   `json:"name"`
	Servers          []Server `json:"servers"`
	ServersChanged   bool     `json:"serversChanged"`
	SslGrade         string   `json:"ssl_grade"`
	PreviousSslGrade string   `json:"previousSslGrade"`
	Logo             string   `json:"logo"`
	Title            string   `json:"title"`
	IsDown           bool     `json:"isDown"`
}

/**
Función encargada de recibir la petición para dar información acerca de un dominio
1. Hace la petición a SSLABS
	si el resultado de la petición es DNS, envia mensaje anunciando
	si el resultado de la petición es IN_PROGRESS envía mensaje anunciando
	si el resultado de la petición es ERROR responde con el error
	si el resultado es READY
		el programa valida de hace cuanto es el reporte que hay en sslabs
		si es de hace más de una hora, hace una nueva petición y le informa al usuario
		de lo contrario valida si los servidores han cambiado respecto a los que ya están y actualiza la información a mostrar

*/
func makeRequest(w http.ResponseWriter, r *http.Request) {

	var dominioToFill Dominio
	ctx := r.Context()
	url := chi.URLParamFromCtx(ctx, "domainName")
	dir := fmt.Sprint("https://api.ssllabs.com/api/v3/analyze?host=" + url)
	client := &http.Client{}
	req, err := http.NewRequest("GET", dir, nil)

	resp, err := client.Do(req)

	if err != nil {
		return
	}

	res, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return

	}

	var mapWithData = make(map[string]interface{})
	err = json.Unmarshal([]byte(res), &mapWithData)

	if err != nil {

		log.Println(err)

	}
	statusMessage := fmt.Sprint(mapWithData["status"])

	if statusMessage == "DNS" {

		w.Write([]byte(fmt.Sprint(mapWithData["statusMessage"], " Wait... and Reload")))
		fmt.Println("dns")

	} else if statusMessage == "IN_PROGRESS" {

		w.Write([]byte("In Progress wait aprox 60 seconds... and Reload"))
		fmt.Println("in pro")

	} else if statusMessage == "ERROR" {

		w.Write([]byte(fmt.Sprint(mapWithData["statusMessage"])))
		return

	} else {

		parche, _ := strconv.ParseFloat(fmt.Sprint(mapWithData["testTime"]), 64)

		testime := int64(parche)
		actualTime := time.Now()
		diferenceTime := actualTime.Sub(time.Unix((testime / 1000), 0))

		hoursDif := diferenceTime.Hours()

		if hoursDif >= 1 {

			newRequestToValidateChangeInServers(dir)
			w.Write([]byte("Validating new changes please wait... and Reload in aprox 60 seconds :)"))

		} else {

			isDom := isDominioInDB(fmt.Sprint(mapWithData["host"]))

			if isDom {

				validateChanges := validateChangesInServers(mapWithData)

				if validateChanges {
					dominioToFill.ServersChanged = true
					getResponseWithJSON(mapWithData, w, false, &dominioToFill, url)
					updateInDatabase(fmt.Sprint(mapWithData["host"]), mapWithData)
				} else {
					dominioToFill.ServersChanged = false
					getResponseWithJSON(mapWithData, w, false, &dominioToFill, url)
				}

			} else {

				getResponseWithJSON(mapWithData, w, true, &dominioToFill, url)

			}

		}

	}

}

/**

Método encargado de validar si los servidores almacenados en la BD son los mismos arrojados por la petición
premisa: se evalúan los servers respecto a su dirección IP, es decir, si tienen la misma IP, entonces son los mismos servidores

*/
func validateChangesInServers(mapWithData map[string]interface{}) bool {

	host := fmt.Sprint(mapWithData["host"])

	databaseReference, err := getDataBase()

	if err == nil {

		for _, value := range mapWithData["endpoints"].([]interface{}) {

			var server Server

			mapu := value.(map[string]interface{})

			server.Address = fmt.Sprint(mapu["ipAddress"])
			query := fmt.Sprint("SELECT servidores->'servers' @> ('[{\"address\": \"", server.Address, "\"}]'::JSONB) FROM dominio where id=", "'", host, "'")
			r, err := databaseReference.Query(query)
			if err != nil {
				log.Println(err)
				return false
			}

			defer r.Close()
			var val bool
			for r.Next() {

				if err := r.Scan(&val); err != nil {

					log.Println(err)
				}
				if val == false {

					return true
					break
				}

			}

		}
	}

	return false
}

/**
Método encargado de hacer una nueva petición a SSLABS
*/
func newRequestToValidateChangeInServers(dir string) {
	client := &http.Client{}

	dir = fmt.Sprint(dir, "&startNew=on")
	req, err := http.NewRequest("GET", dir, nil)

	resp, err := client.Do(req)

	if err != nil {
		return
	}

	res, _ := ioutil.ReadAll(resp.Body)
	if res == nil {
		return

	}

}

/**
Método que se encargada de llenar la información del dominio a mostrar y de dar respuesta al usuario con la info del dominio
*/
func getResponseWithJSON(mapWithData map[string]interface{}, w http.ResponseWriter, saveInDb bool, dominioToFill *Dominio, url string) {

	wg.Add(2)

	go getDataFromSsl(mapWithData, dominioToFill)
	go getHTMLTitleAndIcon(url, dominioToFill)

	wg.Wait()

	jsonToPrint, _ := json.MarshalIndent(dominioToFill, "", "\t")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(jsonToPrint)

	if saveInDb {
		result, err := saveDataInDatabase(dominioToFill.Name, string(jsonToPrint))
		if err != nil {
			return
		}

		fmt.Printf(result)
	}
}

/**
Método encargado de validar si existe el host en la base de datos
*/
func isDominioInDB(host string) bool {

	fmt.Println("entro a is: ")
	databaseReference, err := getDataBase()
	query := fmt.Sprint("SELECT id FROM dominio WHERE id=", "'", host, "'")
	r, err := databaseReference.Query(query)
	if err != nil {
		log.Println(err)
		return false
	}

	var id string
	r.Next()
	r.Scan(&id)
	if id == "" {

		return false
	}

	return true

}

/**
Método encargado de devolver la base de datos para realizar las Query
*/
func getDataBase() (*sql.DB, error) {

	databaseReference, err := sql.Open("postgres",
		"postgresql://truora@localhost:26257/jsonb_dominios?ssl=true&sslmode=require&sslrootcert=/home/sebastian/certs/ca.crt&sslkey=/home/sebastian/certs/client.truora.key&sslcert=/home/sebastian/certs/client.truora.crt")
	if err != nil {
		fmt.Println("error connecting to the database: ", err)
		return nil, err
	}

	return databaseReference, err

}

/**
Método encargado de actualizar la información de los servidores de un host almacenado en la base de datos
*/

func updateInDatabase(dominio string, mapWithData map[string]interface{}) (string, error) {

	var arrayServers []Server
	for _, value := range mapWithData["endpoints"].([]interface{}) {

		var server Server

		mapu := value.(map[string]interface{})

		server.Address = fmt.Sprint(mapu["ipAddress"])

		server.SslGrade = fmt.Sprint(mapu["grade"])

		getCountryAndOwner(&server, fmt.Sprint(mapu["ipAddress"]))

		arrayServers = append(arrayServers, server)

	}

	jsonOfservers, _ := json.Marshal(arrayServers)
	datos := string(jsonOfservers)

	databaseReference, err := getDataBase()

	query := fmt.Sprint("UPDATE dominio SET servidores=", datos, " WHERE id=", "'", dominio, "'")

	r, err := databaseReference.Exec(query)
	if err != nil {
		log.Println(err)
		return "", err
	}

	r.LastInsertId()

	return "guardado exitoso", nil

}

/**
Método encargado de guardar la información del dominio consultado en la base de datos
*/

func saveDataInDatabase(dominio, datos string) (string, error) {
	databaseReference, err := getDataBase()
	query := fmt.Sprint("INSERT INTO jsonb_dominios.dominio (id, servidores) VALUES ('", dominio, "','", datos, "')")

	r, err := databaseReference.Exec(query)
	if err != nil {
		log.Println(err)
		return "", err
	}

	r.LastInsertId()

	return "guardado exitoso", nil

}

/**
Método encargado de validar el formato de la URL, es decir, si la url es truora.com, el método devuelve una url completa con esquema: http://truora.com

*/
func validateURL(link string) string {

	u, err := url.Parse(link)

	if err != nil {
		panic(err)
	}

	if u.Scheme == "" {

		return fmt.Sprint("http://", link)

	}

	return link

}

/**

Método encargado de obtener el título y el Icono y de llenarlo en la respectiva información a mostrar

*/
func getHTMLTitleAndIcon(url string, dominioTofill *Dominio) {

	s, err := scrap.Scrape(validateURL(url), 5)
	if err != nil {
		fmt.Println("aqui ", err)
		wg.Done()
		return
	}

	dominioTofill.Title = s.Preview.Title
	dominioTofill.Logo = s.Preview.Icon

	wg.Done()

}

/**
Método encargado de extraer toda la información proporcionado por SSLLabs y llenarla en la información del dominio a mostrar
*/
func getDataFromSsl(mapWithData map[string]interface{}, dominioToFill *Dominio) {

	dominioToFill.Name = fmt.Sprint(mapWithData["host"])

	var arrayServers []Server
	for _, value := range mapWithData["endpoints"].([]interface{}) {

		var server Server

		mapu := value.(map[string]interface{})

		server.Address = fmt.Sprint(mapu["ipAddress"])

		server.SslGrade = fmt.Sprint(mapu["grade"])

		getCountryAndOwner(&server, fmt.Sprint(mapu["ipAddress"]))

		arrayServers = append(arrayServers, server)

	}

	dominioToFill.SslGrade = getLowestGrade(arrayServers)
	dominioToFill.Servers = arrayServers

	wg.Done()

}

/**
Método encargado de encontrar el servidor con la nota de certificado más baja
*/
func getLowestGrade(arrayServers []Server) string {

	arrayInt := make([]int, len(arrayServers))

	for i := 0; i < len(arrayServers); i++ {

		switch arrayServers[i].SslGrade {
		
		case "A+":
			arrayInt[i] = 85
		case "A":
			arrayInt[i] = 80
		case "B":
			arrayInt[i] = 65
		case "C":
			arrayInt[i] = 50
		case "D":
			arrayInt[i] = 35
		case "E":
			arrayInt[i] = 20
		case "F":
			arrayInt[i] = 15
		case "T":
			arrayInt[i] = 0

		}
	}
	sort.Ints(arrayInt)
	var result string
	switch arrayInt[0] {
	
	case 85:
		result = "A+"
	case 80:
		result = "A"
	case 65:
		result = "B"
	case 50:
		result = "C"
	case 35:
		result = "D"
	case 20:
		result = "E"
	case 15:
		result = "F"
	case 0:
		result = "T"

	}

	return result

}

/**
Método encargado de añadir a la información del dominio a mostrar el respectivo PAIS y ORGANIZACIÓN dueña del servidor
*/

func getCountryAndOwner(serverToFill *Server, ip string) {
	result, err := whois.Whois(ip)
	if err == nil {
		raw, err := whoisparser.Parse(result)
		if err == nil {

			serverToFill.Country = raw.Registrant.Country

			serverToFill.Owner = raw.Registrant.Organization

		}
	}

}

func DominioCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var dominio *Dominio
		//var found bool
		domainName := chi.URLParam(r, "domainName")

		if domainName == "" {

			w.WriteHeader(http.StatusNotFound)
			return

		}

		ctx := context.WithValue(r.Context(), "dominio", dominio)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

/**
Metodo handler para devolver la lista de todos los dominios consultados previamente
*/
func listAll(w http.ResponseWriter, r *http.Request) {

	databaseReference, err := getDataBase()

	if err != nil {

		w.Write([]byte("Lo sentimos vuelvelo a intentar :("))
		return

	}

	query := "SELECT id, jsonb_pretty(servidores) from dominio"

	rs, err := databaseReference.Query(query)

	if err != nil {
		log.Println(err)
		return
	}

	var id string
	var servidores string

	for rs.Next() {

		err := rs.Scan(&id, &servidores)

		if err != nil {
			break
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)

		w.Write([]byte(id))
		w.Write([]byte(servidores))

	}

}

func main() {

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	r.Route("/dominio", func(r chi.Router) {

		r.Route("/{domainName}", func(r chi.Router) {
			r.Use(DominioCtx)
			r.Get("/", makeRequest) // GET /dominio/truora.com

		})
	})
	r.Route("/list", func(r chi.Router) {

		r.Get("/", listAll) // GET /list/

	})

	fmt.Println("Server listen at :8005")
	http.ListenAndServe(":8005", r)

}
