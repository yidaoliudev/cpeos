package main

import (
	"cpeos/agentLog"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/gorilla/mux"
)

type Route struct {
	Name    string
	Method  string
	Path    string
	Handler http.HandlerFunc
}

type Routes []Route

var routes = Routes{
	Route{
		"createPort",
		"POST",
		"/cpe/{sn}/port",
		PostDecorator(CreatePort, ""),
	},
	Route{
		"modPort",
		"PUT",
		"/cpe/{sn}/port",
		PutDecorator(ModPort, ""),
	},
	Route{
		"delPort",
		"DELETE",
		"/cpe/{sn}/port/{id}",
		DelDecorator(DeletePort, ""),
	},
	Route{
		"createSubnet",
		"POST",
		"/cpe/{sn}/subnet",
		PostDecorator(CreateSubnet, ""),
	},
	Route{
		"modSubnet",
		"PUT",
		"/cpe/{sn}/subnet",
		PutDecorator(ModSubnet, ""),
	},
	Route{
		"delSubnet",
		"DELETE",
		"/cpe/{sn}/subnet/{id}",
		DelDecorator(DeleteSubnet, ""),
	},
	Route{
		"createStatic",
		"POST",
		"/cpe/{sn}/static",
		PostDecorator(CreateStatic, ""),
	},
	Route{
		"modStatic",
		"PUT",
		"/cpe/{sn}/static",
		PutDecorator(ModStatic, ""),
	},
	Route{
		"delStatic",
		"DELETE",
		"/cpe/{sn}/static/{id}",
		DelDecorator(DeleteStatic, ""),
	},
	Route{
		"createCheck",
		"POST",
		"/cpe/{sn}/check",
		PostDecorator(CreateCheck, ""),
	},
	Route{
		"modCheck",
		"PUT",
		"/cpe/{sn}/check",
		PutDecorator(ModCheck, ""),
	},
	Route{
		"delCheck",
		"DELETE",
		"/cpe/{sn}/check/{id}",
		DelDecorator(DeleteCheck, ""),
	},
	Route{
		"modBgp",
		"PUT",
		"/cpe/{sn}/bgp",
		PutDecorator(ModBgp, ""),
	},
	Route{
		"modDns",
		"PUT",
		"/cpe/{sn}/dns",
		PutDecorator(ModDns, ""),
	},
	Route{
		"modHa",
		"PUT",
		"/cpe/{sn}/ha",
		PutDecorator(ModHa, ""),
	},
	Route{
		"configAll",
		"POST",
		"/cpe/{sn}/configAll",
		PostDecorator(ConfigAll, ""),
	},
}

func cpeAgentRouter() *mux.Router {
	router := mux.NewRouter()
	for _, route := range routes {
		var handler http.Handler
		handler = route.Handler
		handler = logger(handler)
		router.Methods(route.Method).Path(route.Path).Name(route.Name).Handler(handler)
	}

	return router
}

func logger(handler http.Handler) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			runtime.LockOSThread()
			agentLog.AgentLogger.Info(fmt.Sprintf("%s\t%s\t%s", r.Method, r.RequestURI, r.RemoteAddr))
			defer httpLog(start, w, r)
			handler.ServeHTTP(w, r)
		})
}

func httpLog(start time.Time, w http.ResponseWriter, r *http.Request) {
	if msg := recover(); msg != nil {
		switch t := msg.(type) {
		case Msg:
			msgOb := msg.(Msg)
			//res, err := json.Marshal(msgOb.res)
			agentLog.AgentLogger.Error(fmt.Sprintf("%s\t%s\t%s\t%d\t%s", r.Method, r.RequestURI,
				r.RemoteAddr, http.StatusOK, time.Since(start)))
			agentLog.AgentLogger.Error(msgOb)
			agentLog.AgentLogger.Error(msgOb.Err.Error())
			agentLog.AgentLogger.Error(string(debug.Stack()[:]))
			if msgOb.res.Code == "500" {
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				w.WriteHeader(http.StatusOK)
			}
			res, err := json.Marshal(msgOb.res.Message)
			if _, err = io.WriteString(w, string(res[:])); err != nil {
				agentLog.AgentLogger.Error("write body string error.")
			}
		case error:
			err := msg.(error)
			agentLog.AgentLogger.Error(fmt.Sprintf("%s\t%s\t%s\t%d\t%s", r.Method, r.RequestURI,
				r.RemoteAddr, http.StatusOK, time.Since(start)))
			agentLog.AgentLogger.Error(err.Error())
			agentLog.AgentLogger.Error(string(debug.Stack()[:]))
			if _, err = io.WriteString(w, `{"success": false, "code": "InternalError", }`); err != nil {
				agentLog.AgentLogger.Error("write body string error.")
			}
		default:
			agentLog.AgentLogger.Error(fmt.Sprintf("Error Type : %T", t))
			agentLog.AgentLogger.Error(fmt.Sprintf("%s\t%s\t%s\t%d\t%s", r.Method, r.RequestURI,
				r.RemoteAddr, http.StatusInternalServerError, time.Since(start)))
			agentLog.AgentLogger.Error(string(debug.Stack()[:]))
			http.Error(w, InternalError, http.StatusInternalServerError)
		}
	} else {
		agentLog.AgentLogger.Info(fmt.Sprintf("%s\t%s\t%s\t%d\t%s", r.Method, r.RequestURI,
			r.RemoteAddr, http.StatusOK, time.Since(start)))
	}
}
