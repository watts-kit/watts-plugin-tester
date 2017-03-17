package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	v "github.com/gima/govalid/v1"
	"github.com/imdario/mergo"
	"github.com/kalaspuffar/base64url"
	"gopkg.in/alecthomas/kingpin.v2"
	"io/ioutil"
	"os"
	"os/exec"
)


type PluginInput map[string](*json.RawMessage)

type Output map[string](*json.RawMessage)

var (
	version = "0.1.0"

	app                 = kingpin.New("watts-plugin-tester", "Test tool for watts plugins")
	pluginTestAction    = app.Flag("plugin-action", "The plugin action to be tested. Defaults to \"parameter\"").Default("parameter").Short('a').String()
	printVersion        = app.Command("version", "Print the version information")
	pluginInputOverride = app.Flag("json", "Use user provided json to override the inbuilt one").Short('j').String()
	//verbose = app.Flag("verbose", "Be verbose").Short('v').Bool()
	machineReadable = app.Flag("machine", "Be machine readable (all output will be json)").Short('m').Bool()

	pluginTest     = app.Command("test", "Test a plugin")
	pluginTestName = pluginTest.Arg("pluginName", "Name of the plugin to test").Required().String()

	printDefault     = app.Command("default", "Print the default plugin input as json")
	validateDefault  = printDefault.Flag("validate", "Validate the produced json").Short('v').Bool()
	printSpecific    = app.Command("specific", "Print the plugin input (including the user override) as json")
	validateSpecific = printSpecific.Flag("validate", "Validate the produced json").Short('v').Bool()

	outputMessages = []json.RawMessage{}

	defaultWattsVersion     = json.RawMessage(`"1.0.0"`)
	defaultCredentialState  = json.RawMessage(`"undefined"`)
	defaultConfParams       = json.RawMessage(`{}`)
	defaultParams           = json.RawMessage(`{}`)
	defaultAdditionalLogins = json.RawMessage(`[]`)
	defaultUserInfo         = json.RawMessage(`{
		"iss": "https://issuer.example.com",
		"sub": "123456789"
	}`)

	defaultAction      = json.RawMessage(`"parameter"`)
	defaultWattsUserId = json.RawMessage(``)

	defaultPluginInput = PluginInput{
		"watts_version":     &defaultWattsVersion,
		"cred_state":        &defaultCredentialState,
		"conf_params":       &defaultConfParams,
		"params":            &defaultParams,
		"user_info":         &defaultUserInfo,
		"additional_logins": &defaultAdditionalLogins,
	}

	schemeAccessToken = v.Optional(v.String())
	schemeUserInfo    = v.Object(
		v.ObjKV("iss", v.String()),
		v.ObjKV("sub", v.String()),
	)
	schemeAdditionalLogins = v.Array(v.ArrEach(
		v.Object(
			v.ObjKV("user_info", schemeUserInfo),
			v.ObjKV("access_token", schemeAccessToken),
		),
	))
	schemeParams = v.Object(
		v.ObjKeys(v.String(v.StrRegExp("^[a-z0-9_]+$"))),
	)
	pluginInputScheme = v.Object(
		v.ObjKV("watts_version", v.String()),
		v.ObjKV("watts_userid", v.String()),
		v.ObjKV("cred_state", v.String()),
		v.ObjKV("access_token", schemeAccessToken),
		v.ObjKV("additional_logins", schemeAdditionalLogins),
		v.ObjKV("conf_params", schemeParams),
		v.ObjKV("params", schemeParams),
		v.ObjKV("user_info", schemeUserInfo),
	)

	schemes = map[string]v.Validator{
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
		"revoke": v.Or(
			v.Object(
				v.ObjKV("result", v.String(v.StrIs("ok"))),
			),
			v.Object(
				v.ObjKV("result", v.String(v.StrIs("error"))),
				v.ObjKV("user_msg", v.String()),
				v.ObjKV("log_msg", v.String()),
			),
		),
	}
)

func (p *PluginInput) validate() {
	var er error
	var bs []byte
	var i interface{}

	bs, er = json.MarshalIndent(p, "", "    ")
	if er != nil {
		//TODO rework output
		fmt.Printf("--- plugin input:\n%s\n", *p)
		fmt.Printf("--- bytes:\n%s\n", bs)
		fmt.Printf("---error marshaling:\n%s\n", er)
		os.Exit(1)
	}

	json.Unmarshal(bs, &i)
	path, err := pluginInputScheme.Validate(i)

	if err != nil {
		fmt.Printf("Unable to validate plugin input\n")
		fmt.Printf("%s: %s\n", "Plugin Input", bs)
		fmt.Printf("%s: %s\n", "Error", err)
		fmt.Printf("%s: %s\n", "Path", path)
		os.Exit(1)
	} else {
		if *validateSpecific || *validateDefault {
			fmt.Printf("Plugin input validation passed\n")
		}
	}

	return
}

func (p *PluginInput) generateUserID() {
	userIdJson := map[string](*json.RawMessage){}
	userIdJsonReduced := map[string](*json.RawMessage){}

	userInfo := *(*p)["user_info"]
	//fmt.Printf("user_info: %s\n", userInfo)

	err := json.Unmarshal(userInfo, &userIdJson)
	if err != nil {
		fmt.Printf("Error unmarshaling watts_userid: %s\n", err)
		os.Exit(1)
	}

	//fmt.Printf("uid:%s\n", userIdJson)

	userIdJsonReduced["iss"] = userIdJson["iss"]
	userIdJsonReduced["sub"] = userIdJson["sub"]

	j, err := json.Marshal(userIdJsonReduced)
	//fmt.Printf("reduced uid:%s\n", j)

	escaped := bytes.Replace(j, []byte{'/'}, []byte{'\\', '/'}, -1)
	st := fmt.Sprintf("\"%s\"", base64url.Encode(escaped))
	defaultWattsUserId = json.RawMessage(st)
	(*p)["watts_userid"] = &defaultWattsUserId
	return
}

func (p *PluginInput) marshalPluginInput() (s []byte) {
	var err error

	s, err = json.MarshalIndent(*p, "    ", "    ")
	if err != nil {
		fmt.Printf("Unable to marshal: Error (%s)\n%s\n", err, s)
		os.Exit(1)
	}
	return
}

func (p *PluginInput) specifyPluginInput(path string) {
	p = &defaultPluginInput

	if path == "" {
		return
	}

	overrideBytes, err := ioutil.ReadFile(*pluginInputOverride)
	if err != nil {
		fmt.Println("Error reading user provided file ", *pluginInputOverride, " (", err, ")")
		os.Exit(1)
	}

	overridePluginInput := PluginInput{}
	err = json.Unmarshal(overrideBytes, &overridePluginInput)
	if err != nil {
		fmt.Println("Error unmarshaling user provided json: ", *pluginInputOverride, " (", err, ")")
		os.Exit(1)
	}

	err = mergo.Merge(&overridePluginInput, p)
	if err != nil {
		fmt.Println("Error merging: (", err, ")")
		os.Exit(1)
	}

	*p = overridePluginInput
	return
}

func doPluginTest(pluginName string, pluginInputJson []byte) (output Output) {
	output = Output{}

	output.print("plugin_name", pluginName)
	output.print("action", *pluginTestAction)
	output.printJson("input", json.RawMessage(pluginInputJson))

	inputBase64 := base64.StdEncoding.EncodeToString(pluginInputJson)
	out, err := exec.Command(pluginName, inputBase64).CombinedOutput()
	if err != nil {
		output.print("result", "error")
		output.print("description", "error executing the plugin")
		return
	}

	var pluginOutputJson interface{}
	json.Unmarshal(out, &pluginOutputJson)

	b, _ := json.MarshalIndent(&pluginOutputJson, "    ", "    ")
	output.printJson("output", json.RawMessage(b))

	path, errr := schemes[*pluginTestAction].Validate(pluginOutputJson)
	if errr != nil {
		output.print("result", "error")
		output.print("description", fmt.Sprintf("Validation error at %s. Error (%s)", path, errr))
		return
	}

	output.print("result", "ok")
	output.print("description", "validation passed")
	return
}

func (o *Output) printJson(a string, b json.RawMessage) {
	outputMessages = append(outputMessages, b)
	(*o)[a] = &(outputMessages[len(outputMessages) -1])

	if !*machineReadable {
		fmt.Printf("%15s:\n%s\n", a, string(b))
	}
}
func (o *Output) print(a string, b string) {
	m := json.RawMessage(fmt.Sprintf("\"%s\"", b))
	outputMessages = append(outputMessages, m)
	(*o)[a] = &(outputMessages[len(outputMessages) -1])

	if !*machineReadable {
		fmt.Printf("%15s: %s\n", a, b)
	}
}

func printOutput(o Output) {
	if *machineReadable {
		bs, err := json.MarshalIndent(&o, "", "    ")
		if err != nil {
			/*
			TODO check if the handling of this case is needed
			var eo ErrorOutput
			eo.Meta = o.Meta
			eo.InvalidOutput = string(o.Output)
			eo.ErrorString = fmt.Sprintf("%s", err)

			bss, errr := json.MarshalIndent(&eo, "", "    ")
			if errr == nil {
				fmt.Printf("%s", string(bss))
			} else {
				fmt.Println("watts-plugin-tester: ERROR")
			}

			*/
		}
		fmt.Printf("%s", string(bs))
	}
	return
}

/*
* all plugin input generation shall take place here
 */
func main() {
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case pluginTest.FullCommand():
		defaultPluginInput.specifyPluginInput(*pluginInputOverride)

		defaultAction = json.RawMessage(fmt.Sprintf("\"%s\"", *pluginTestAction))
		defaultPluginInput["action"] = &defaultAction

		defaultPluginInput.generateUserID()

		defaultPluginInput.validate()
		pluginInputJson := defaultPluginInput.marshalPluginInput()

		o := doPluginTest(*pluginTestName, pluginInputJson)
		printOutput(o)

	case printDefault.FullCommand():
		if *validateDefault {
			defaultPluginInput.validate()
		}

		fmt.Printf("%s", defaultPluginInput.marshalPluginInput())

	case printSpecific.FullCommand():
		defaultPluginInput.specifyPluginInput(*pluginInputOverride)
		defaultPluginInput.generateUserID()
		if *validateSpecific {
			defaultPluginInput.validate()
		}

		fmt.Printf("%s", defaultPluginInput.marshalPluginInput())

	case printVersion.FullCommand():
		fmt.Printf("%s\n", version)

	}
}
