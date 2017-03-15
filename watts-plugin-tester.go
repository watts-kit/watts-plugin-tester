package main

import (
	"os"
	"os/exec"
	"fmt"
	"gopkg.in/alecthomas/kingpin.v2"
	"encoding/base64"
	"encoding/json"
	v "github.com/gima/govalid/v1"
	"io/ioutil"
	"github.com/imdario/mergo"
)

type UserInfo struct {
	FamilyName string `json:"family_name"`
	Gender string `json:"gender"`
	GivenName string`json:"given_name"`
	ISS string `json:"iss"`
	Name string `json:"name"`
	Sub string `json:"sub"`
}

type PluginInput struct {
	WattsVersion string `json:"watts_version"`
	Action string `json:"action"`
	ConfParams string `json:"conf_params"`
	Params string `json:"params"`
	CredState string `json:"cred_state"`
	UserInformation UserInfo `json:"user_info"`
	WattsUserid string `json:"watts_userid"`
}


var (
	app = kingpin.New("watts-plugin-tester", "Test tool for watts plugins")
	pluginTestAction = app.Flag("plugin-action", "The plugin action to be tested. Defaults to \"parameter\"").Default("parameter").Short('a').String()
	pluginInputOverride = app.Flag("json", "Use user provided json to override the inbuilt one").Short('j').String()
	verbose = app.Flag("verbose", "Be verbose").Short('v').Bool()

	pluginTest = app.Command("test", "Test a plugin")
	pluginTestName = pluginTest.Arg("pluginName", "Name of the plugin to test").Required().String()

	printDefault = app.Command("default", "Print the default plugin input as json")
	printSpecific = app.Command("specific", "Print the plugin input (including the user override) as json")

	userId = "max_mustermann"
	defaultPluginInputTemplate = PluginInput{
		WattsVersion: "1.0",
		ConfParams: "{}",
		Params: "{}",
		CredState: "undefined",
		UserInformation: UserInfo{
			FamilyName: "Mustermann",
			Gender: "Male",
			GivenName: "Max",
			ISS: "https://issuer.example.com",
			Name: "Max Mustermann",
			Sub: "123456789",
		},
	}

	schemes =  map[string]v.Validator{
		"parameter": v.Object(
			v.ObjKV("result", v.String(v.StrIs("ok"))),
			v.ObjKV("conf_params", v.Array(v.ArrEach(
				v.Object(
					v.ObjKV("name", v.String()),
					v.ObjKV("type", v.String()),
					v.ObjKV("default", v.String()),
				),
			))),
			v.ObjKV("request_params", v.Array(v.ArrEach(
				v.Array(v.ArrEach(
					v.Object(
						v.ObjKV("key", v.String()),
						v.ObjKV("name", v.String()),
						v.ObjKV("description", v.String()),
						v.ObjKV("type", v.String()),
						v.ObjKV("mandatory", v.Boolean()),
					),
				)),
			))),
			v.ObjKV("version", v.String()),
		),
		"request": v.Or(
			v.Object(
				v.ObjKV("result", v.String(v.StrIs("ok"))),
				v.ObjKV("credential", v.Array(v.ArrEach(
					v.Object(
						v.ObjKV("name", v.String()),
						v.ObjKV("type", v.String()),
						v.ObjKV("value", v.String()),
					),
				))),
				v.ObjKV("state", v.String()),
			),
			v.Object(
				v.ObjKV("result", v.String(v.StrIs("error"))),
				v.ObjKV("user_msg", v.String()),
				v.ObjKV("log_msg", v.String()),
			),
		),
	}
)

func defaultPluginInput() (pluginInput PluginInput) {
	pluginInput = defaultPluginInputTemplate
	pluginInput.Action = *pluginTestAction
	pluginInput.WattsUserid = base64.StdEncoding.EncodeToString([]byte(userId))
	return
}

func defaultJson() (s []byte) {
	s, _ = json.Marshal(defaultPluginInput())
	return
}

func specificJson() (inputJson []byte) {
	inputJson = defaultJson()

	if *pluginInputOverride != "" {
		inputOverride, err := ioutil.ReadFile(*pluginInputOverride)
		if err !=  nil {
			fmt.Println("Error reading file ", *pluginInputOverride, " (", err, ")")
			return
		}

		var overrideJson PluginInput
		errr := json.Unmarshal(inputOverride, &overrideJson)
		if errr != nil {
			return
		}

		mergo.Merge(&overrideJson, defaultJson())

		inputJson, _ = json.Marshal(overrideJson)
	}
	return
}

func doPluginTestAction(pluginName string) {
	fmt.Println("testing ", pluginName, "->", *pluginTestAction)

	inputBase64 := base64.StdEncoding.EncodeToString(specificJson())

	out, err := exec.Command(pluginName, inputBase64).Output()
	if err != nil {
		fmt.Println("Error executing command: ", err)
		return
	}

	if *verbose {
		fmt.Println("output: ", string(out))
	}


	var pluginOutput interface{}
	json.Unmarshal(out, &pluginOutput)

	path, errr := schemes[*pluginTestAction].Validate(pluginOutput)
	if errr == nil {
		fmt.Println("Validation passed")
	} else {
		fmt.Printf("Validation error at %s. Error (%s)", path, errr)
	}

	return
}

func main() {
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case pluginTest.FullCommand():
		doPluginTestAction(*pluginTestName)
	case printDefault.FullCommand():
		fmt.Printf("%s", string(defaultJson()))
	case printSpecific.FullCommand():
		fmt.Printf("%s", string(specificJson()))
	}
}
