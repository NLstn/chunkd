package api

import (
	"errors"
	"net/http"
	"reflect"

	"github.com/NLstn/chunkd/internal/domain"
	"github.com/NLstn/chunkd/internal/minecraft"
	"github.com/NLstn/chunkd/internal/supervisor"
	odata "github.com/nlstn/go-odata"
)

func (a *app) registerDetails() error {
	for _, def := range []odata.FunctionDefinition{
		{Name: "GetServerDetails", ReturnType: reflect.TypeOf(supervisor.Details{}), Parameters: []odata.ParameterDefinition{{Name: "ServerID", Type: reflect.TypeOf(int64(0)), Required: true}, {Name: "After", Type: reflect.TypeOf(int64(0)), Required: true}}, Handler: func(w http.ResponseWriter, r *http.Request, _ any, p map[string]any) (any, error) {
			id := p["ServerID"].(int64)
			after := p["After"].(int64)
			if id < 1 || after < 0 {
				return nil, odata.NewHookError(400, "Invalid server or log offset")
			}
			result, err := a.minecraft.Details(uint(id), after)
			if err != nil {
				return nil, detailsError(err)
			}
			return result, nil
		}},
		{Name: "GetServerSettings", ReturnType: reflect.TypeOf(minecraft.Settings{}), Parameters: []odata.ParameterDefinition{{Name: "ServerID", Type: reflect.TypeOf(int64(0)), Required: true}}, Handler: func(w http.ResponseWriter, r *http.Request, _ any, p map[string]any) (any, error) {
			id := p["ServerID"].(int64)
			if id < 1 {
				return nil, odata.NewHookError(400, "Invalid server")
			}
			result, err := a.minecraft.Settings(uint(id))
			if err != nil {
				return nil, detailsError(err)
			}
			return result, nil
		}},
	} {
		if err := a.service.RegisterFunction(def); err != nil {
			return err
		}
	}
	if err := a.service.RegisterAction(odata.ActionDefinition{Name: "SendCommand", IsBound: true, EntitySet: "Servers", Parameters: []odata.ParameterDefinition{{Name: "Command", Type: reflect.TypeOf(""), Required: true}}, Handler: func(w http.ResponseWriter, r *http.Request, entity any, p map[string]any) error {
		if err := a.minecraft.Command(entity.(*domain.Server).ID, p["Command"].(string)); err != nil {
			return jobResponse(w, domain.Job{}, err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}}); err != nil {
		return err
	}
	params := []odata.ParameterDefinition{}
	for _, name := range []string{"MOTD", "Difficulty", "GameMode"} {
		params = append(params, odata.ParameterDefinition{Name: name, Type: reflect.TypeOf(""), Required: true})
	}
	for _, name := range []string{"MaxPlayers", "ViewDistance"} {
		params = append(params, odata.ParameterDefinition{Name: name, Type: reflect.TypeOf(int64(0)), Required: true})
	}
	for _, name := range []string{"PVP", "WhiteList"} {
		params = append(params, odata.ParameterDefinition{Name: name, Type: reflect.TypeOf(false), Required: true})
	}
	return a.service.RegisterAction(odata.ActionDefinition{Name: "SaveSettings", IsBound: true, EntitySet: "Servers", Parameters: params, Handler: func(w http.ResponseWriter, r *http.Request, entity any, p map[string]any) error {
		err := a.minecraft.SaveSettings(entity.(*domain.Server).ID, minecraft.Settings{MOTD: p["MOTD"].(string), Difficulty: p["Difficulty"].(string), GameMode: p["GameMode"].(string), MaxPlayers: p["MaxPlayers"].(int64), ViewDistance: p["ViewDistance"].(int64), PVP: p["PVP"].(bool), WhiteList: p["WhiteList"].(bool)})
		if err != nil {
			return jobResponse(w, domain.Job{}, err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}})
}

func detailsError(err error) error {
	switch {
	case errors.Is(err, minecraft.ErrNotFound):
		return odata.NewHookError(404, err.Error())
	case errors.Is(err, minecraft.ErrInvalid):
		return odata.NewHookError(400, err.Error())
	case errors.Is(err, minecraft.ErrConflict):
		return odata.NewHookError(409, err.Error())
	default:
		return err
	}
}
