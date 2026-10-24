package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"

	"github.com/NLstn/chunkd/internal/domain"
	"github.com/NLstn/chunkd/internal/installer"
	"github.com/NLstn/chunkd/internal/javaruntime"
	"github.com/NLstn/chunkd/internal/minecraft"
	odata "github.com/nlstn/go-odata"
)

var lifecyclePath = regexp.MustCompile(`^/odata/Servers\([1-9][0-9]*\)/(?:Chunkd\.)?(Start|Stop|Restart|RetryInstall)$`)

func (a *app) registerMinecraft() error {
	if err := a.service.RegisterFunction(odata.FunctionDefinition{Name: "GetJavaRuntimes", ReturnType: reflect.TypeOf([]javaruntime.Runtime{}), Handler: func(w http.ResponseWriter, r *http.Request, _ any, _ map[string]any) (any, error) {
		return javaruntime.Detect(r.Context()), nil
	}}); err != nil {
		return err
	}
	if err := a.service.RegisterFunction(odata.FunctionDefinition{Name: "GetMinecraftVersions", ReturnType: reflect.TypeOf([]installer.Version{}), Handler: func(w http.ResponseWriter, r *http.Request, _ any, _ map[string]any) (any, error) {
		return a.minecraft.Versions(r.Context())
	}}); err != nil {
		return err
	}
	parameters := []odata.ParameterDefinition{}
	for _, name := range []string{"Name", "Software", "Version", "JavaPath"} {
		parameters = append(parameters, odata.ParameterDefinition{Name: name, Type: reflect.TypeOf(""), Required: true})
	}
	for _, name := range []string{"MemoryMB", "Port"} {
		parameters = append(parameters, odata.ParameterDefinition{Name: name, Type: reflect.TypeOf(int64(0)), Required: true})
	}
	parameters = append(parameters, odata.ParameterDefinition{Name: "EULAAccepted", Type: reflect.TypeOf(false), Required: true})
	if err := a.service.RegisterAction(odata.ActionDefinition{Name: "CreateServer", Parameters: parameters, ReturnType: reflect.TypeOf(domain.Job{}), Handler: func(w http.ResponseWriter, r *http.Request, _ any, p map[string]any) error {
		job, err := a.minecraft.Create(r.Context(), minecraft.CreateInput{Name: p["Name"].(string), Software: p["Software"].(string), Version: p["Version"].(string), JavaPath: p["JavaPath"].(string), MemoryMB: p["MemoryMB"].(int64), Port: p["Port"].(int64), EULAAccepted: p["EULAAccepted"].(bool)})
		return jobResponse(w, job, err)
	}}); err != nil {
		return err
	}
	for _, name := range []string{"Start", "Stop", "Restart", "RetryInstall"} {
		if err := a.service.RegisterAction(odata.ActionDefinition{Name: name, IsBound: true, EntitySet: "Servers", ReturnType: reflect.TypeOf(domain.Job{}), Handler: func(w http.ResponseWriter, r *http.Request, entity any, _ map[string]any) error {
			s := entity.(*domain.Server)
			job, err := a.minecraft.Action(s.ID, name)
			return jobResponse(w, job, err)
		}}); err != nil {
			return err
		}
	}
	return nil
}

func jobResponse(w http.ResponseWriter, job domain.Job, err error) error {
	if err != nil {
		switch {
		case errors.Is(err, minecraft.ErrInvalid):
			fail(w, 400, "BadRequest", err.Error())
		case errors.Is(err, minecraft.ErrConflict):
			fail(w, 409, "Conflict", err.Error())
		case errors.Is(err, minecraft.ErrNotFound):
			fail(w, 404, "NotFound", err.Error())
		default:
			return err
		}
		return nil
	}
	w.Header().Set("Content-Type", "application/json;odata.metadata=minimal")
	w.Header().Set("Location", fmt.Sprintf("/odata/Jobs(%d)", job.ID))
	w.Header().Set("Retry-After", "1")
	w.WriteHeader(http.StatusAccepted)
	return json.NewEncoder(w).Encode(struct {
		Context string `json:"@odata.context"`
		domain.Job
	}{Context: "/odata/$metadata#Jobs/$entity", Job: job})
}

func (a *app) Close() error { return a.minecraft.Close() }
