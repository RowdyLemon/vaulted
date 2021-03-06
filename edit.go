package main

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/miquella/ask"
	"github.com/miquella/vaulted/lib"
	"golang.org/x/crypto/ssh"
)

var (
	green = color.New(color.FgGreen)
	cyan  = color.New(color.FgCyan)
	blue  = color.New(color.FgBlue)

	faintColor   = color.New(color.Faint)
	menuColor    = color.New(color.FgHiBlue)
	warningColor = color.New(color.FgHiYellow)

	ErrExists      = errors.New("Vault exists. Use `vaulted edit` to edit existing vaults.")
	ErrAbort       = errors.New("Aborted by user. Vault unchanged.")
	ErrSaveAndExit = errors.New("Exiting at user request.")
)

type Edit struct {
	New        bool
	VaultName  string
	rlMenu     *readline.Instance
	rlValue    *readline.Instance
	showHidden bool
}

func (e *Edit) Run(steward Steward) error {
	var password string
	var vault *vaulted.Vault
	var err error

	if e.New {
		if vaulted.VaultExists(e.VaultName) {
			return ErrExists
		}

		vault = &vaulted.Vault{}

		creds, err := e.importExistingCreds()
		if err != nil {
			return err
		}

		if creds != nil {
			vault.AWSKey = &vaulted.AWSKey{
				AWSCredentials: *creds,
			}
		}

	} else {
		password, vault, err = steward.OpenVault(e.VaultName, nil)
		if err != nil {
			return err
		}
	}

	err = e.edit(e.VaultName, vault)
	if err != nil {
		return err
	}

	var newPassword *string
	if password != "" {
		newPassword = &password
	}
	err = steward.SealVault(e.VaultName, newPassword, vault)
	if err != nil {
		return err
	}
	fmt.Printf("Vault '%s' successfully saved!\n", e.VaultName)

	return nil
}

func mainMenu() {
	menuColor.Set()
	defer color.Unset()

	output("a - AWS Key")
	output("s - SSH Keys")
	output("v - Variables")
	output("d - Environment Duration")
	output("S - Show/Hide Secrets")
	output("? - Help")
	output("q - Quit")
}

func awsMenu() {
	menuColor.Set()
	defer color.Unset()

	output("k - Key")
	output("m - MFA")
	output("r - Role")
	output("t - Substitute with temporary credentials")
	output("S - Show/Hide Secrets")
	output("D - Delete")
	output("? - Help")
	output("b - Back")
	output("q - Quit")
}

func sshKeysHelp() {
	menuColor.Set()
	defer color.Unset()

	output("a - Add")
	output("D - Delete")
	output("? - Help")
	output("b - Back")
	output("q - Quit")
}

func variableMenu() {
	color.Set(color.FgYellow)
	output("")
	output("a - Add")
	output("S - Show/Hide Secrets")
	output("D - Delete")
	output("? - Help")
	output("b - Back")
	output("q - Quit")
	color.Unset()
}

func (e *Edit) importExistingCreds() (*vaulted.AWSCredentials, error) {
	creds, err := defaults.Get().Config.Credentials.Get()
	if err != nil {
		return nil, nil
	}

	if creds.SessionToken != "" {
		warningColor.Println("There appear to be AWS session credentials in your current environment.")
		warningColor.Println("Vaulted cannot import AWS session credentials.")
		return nil, nil
	}

	for {
		warningColor.Println("There appear to be AWS credentials in your current environment.")
		input, err := e.readPrompt("Would you like to import these credentials? (Y/n): ")
		if err != nil {
			return nil, err
		}

		switch strings.ToLower(input) {
		case "", "y", "yes":
			return &vaulted.AWSCredentials{
				ID:     creds.AccessKeyID,
				Secret: creds.SecretAccessKey,
			}, nil

		case "n", "no":
			return nil, nil

		default:
			output("")
			color.Red("Response not recognized. Please enter 'y' or 'n'.")
			output("")
			continue
		}
	}
}

func (e *Edit) edit(name string, v *vaulted.Vault) error {
	var err error

	for {
		cyan.Printf("\nVault: ")
		fmt.Printf("%s", name)
		e.printVariables(v)
		e.printAWS(v)
		e.printSSHKeys(v)
		e.printDuration(v)

		var input string
		input, err = e.readMenu("Edit vault: [a,s,v,d,S,?,q]: ")
		if err != nil {
			break
		}
		switch input {
		case "a", "aws":
			err = e.aws(v)
		case "s", "ssh":
			err = e.sshKeysMenu(v)
		case "v", "vars", "variables":
			err = e.variables(v)
		case "d", "duration":
			e.setDuration(v)
		case "S", "show", "hide":
			e.showHidden = !e.showHidden
		case "b", "q", "quit", "exit":
			return nil
		case "?", "help":
			mainMenu()
		default:
			color.Red("Command not recognized")
		}

		if err != nil {
			break
		}
	}

	if err == io.EOF || err == ErrSaveAndExit {
		return nil
	} else {
		return err
	}
}

func (e *Edit) aws(v *vaulted.Vault) error {
	var err error

	for {
		var input string
		e.printAWS(v)
		if v.AWSKey == nil {
			input, err = e.readMenu("Edit AWS key [k,?,b,q]: ")
		} else {
			input, err = e.readMenu("Edit AWS key [k,m,r,t,S,D,?,b,q]: ")
		}

		if err != nil {
			return err
		}

		switch input {
		case "k", "add", "key", "keys":
			warningColor.Println("Note: For increased security, Vaulted defaults to substituting your credentials with temporary credentials.")
			warningColor.Println("      The key specified here may not match the key in your spawned session.")
			output("")

			awsAccesskey, keyErr := e.readValue("Key ID: ")
			if keyErr != nil {
				return keyErr
			}
			awsSecretkey, secretErr := e.readValue("Secret: ")
			if secretErr != nil {
				return secretErr
			}
			v.AWSKey = &vaulted.AWSKey{
				AWSCredentials: vaulted.AWSCredentials{
					ID:     awsAccesskey,
					Secret: awsSecretkey,
				},
				MFA:  "",
				Role: "",
				ForgoTempCredGeneration: false,
			}
		case "m", "mfa":
			if v.AWSKey != nil {
				var awsMfa string
				awsMfa, err = e.readValue("MFA ARN or serial number: ")
				if err == nil {
					v.AWSKey.MFA = awsMfa
				}
			} else {
				color.Red("Must associate an AWS key with the vault first")
			}
		case "r", "role":
			if v.AWSKey != nil {
				var awsRole string
				awsRole, err = e.readValue("Role ARN: ")
				if err == nil {
					v.AWSKey.Role = awsRole
				}
			} else {
				color.Red("Must associate an AWS key with the vault first")
			}
		case "t", "temp", "temporary":
			if v.AWSKey != nil {
				forgoTempCredGeneration := !v.AWSKey.ForgoTempCredGeneration
				if !forgoTempCredGeneration && v.Duration > 36*time.Hour {
					var conf string
					warningColor.Println("Proceeding will adjust your vault duration to 36h (the maximum when using temporary creds).")
					conf, err = e.readPrompt("Do you wish to proceed? (y/n): ")
					if conf == "y" {
						v.Duration = 36 * time.Hour
					} else {
						output("Temporary credentials not enabled.")
						continue
					}
				}

				v.AWSKey.ForgoTempCredGeneration = forgoTempCredGeneration
			} else {
				color.Red("Must associate an AWS key with the vault first")
			}
		case "S", "show", "hide":
			e.showHidden = !e.showHidden
		case "D", "delete", "remove":
			if v.AWSKey != nil {
				var removeKey string
				removeKey, err = e.readValue("Delete your AWS key? (y/n): ")
				if err == nil {
					if removeKey == "y" {
						v.AWSKey = nil
					}
				}
			} else {
				color.Red("Must associate an AWS key with the vault first")
			}
		case "b", "back":
			return nil
		case "q", "quit", "exit":
			var confirm string
			confirm, err = e.readValue("Are you sure you wish to exit the vault? (y/n): ")
			if err == nil {
				if confirm == "y" {
					return ErrSaveAndExit
				}
			}
		case "?", "help":
			awsMenu()
		default:
			color.Red("Command not recognized")
		}

		if err != nil {
			return err
		}
	}
	return nil
}

func (e *Edit) sshKeysMenu(v *vaulted.Vault) error {
	for {
		var err error
		e.printSSHKeys(v)
		input, err := e.readMenu("Edit ssh keys: [a,D,?,b,q]: ")
		if err != nil {
			return err
		}
		switch input {
		case "a", "add", "key", "keys":
			err = e.addSSHKey(v)
		case "D", "delete", "remove":
			var key string
			key, err = e.readValue("Key: ")
			if err == nil {
				if _, exists := v.SSHKeys[key]; exists {
					delete(v.SSHKeys, key)
				} else {
					color.Red("Key '%s' not found", key)
				}
			}
		case "b", "back":
			return nil
		case "q", "quit", "exit":
			var confirm string
			confirm, err = e.readValue("Are you sure you wish to exit the vault? (y/n): ")
			if err == nil {
				if confirm == "y" {
					return ErrSaveAndExit
				}
			}
		case "?", "help":
			sshKeysHelp()
		default:
			color.Red("Command not recognized")
		}

		if err != nil {
			return err
		}
	}
	return nil
}

func (e *Edit) addSSHKey(v *vaulted.Vault) error {
	var err error

	homeDir := ""
	user, err := user.Current()
	if err == nil {
		homeDir = user.HomeDir
	} else {
		homeDir = os.Getenv("HOME")
	}

	defaultFilename := ""
	filename := ""
	if homeDir != "" {
		defaultFilename = filepath.Join(homeDir, ".ssh", "id_rsa")
		filename, err = e.readValue(fmt.Sprintf("Key file (default: %s): ", defaultFilename))
		if err != nil {
			return err
		}
		if filename == "" {
			filename = defaultFilename
		}
		if !filepath.IsAbs(filename) {
			filename = filepath.Join(filepath.Join(homeDir, ".ssh"), filename)
		}
	} else {
		filename, err = e.readValue("Key file: ")
		if err != nil {
			return err
		}
	}

	decryptedBlock, err := loadAndDecryptKey(filename)
	if err != nil {
		color.Red("%v", err)
		return nil
	}

	comment := loadPublicKeyComment(filename + ".pub")
	var name string
	if comment != "" {
		name, err = e.readValue(fmt.Sprintf("Name (default: %s): ", comment))
		if err != nil {
			return err
		}
		if name == "" {
			name = comment
		}
	} else {
		name, err = e.readValue("Name: ")
		if err != nil {
			return err
		}
		if name == "" {
			name = filename
		}
	}

	if v.SSHKeys == nil {
		v.SSHKeys = make(map[string]string)
	}
	v.SSHKeys[name] = string(pem.EncodeToMemory(decryptedBlock))

	return nil
}

func loadAndDecryptKey(filename string) (*pem.Block, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, err
	}

	if x509.IsEncryptedPEMBlock(block) {
		var passphrase string
		var decryptedBytes []byte
		for i := 0; i < 3; i++ {
			passphrase, err = ask.HiddenAsk("Passphrase: ")
			if err != nil {
				return nil, err
			}

			decryptedBytes, err = x509.DecryptPEMBlock(block, []byte(passphrase))
			if err == nil {
				break
			}
			if err != x509.IncorrectPasswordError {
				return nil, err
			}
		}

		if err != nil {
			return nil, err
		}

		return &pem.Block{
			Type:  block.Type,
			Bytes: decryptedBytes,
		}, nil
	}
	return block, nil
}

func loadPublicKeyComment(filename string) string {
	f, err := os.Open(filename)
	if err != nil {
		return ""
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return ""
	}

	_, comment, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return ""
	}
	return comment
}

func (e *Edit) variables(v *vaulted.Vault) error {
	var varErr error

	for {
		var input string
		e.printVariables(v)
		if v.Vars == nil {
			input, varErr = e.readMenu("Edit environment variables: [a,?,b,q]: ")
		} else {
			input, varErr = e.readMenu("Edit environment variables: [a,S,D,?,b,q]: ")
		}
		if varErr != nil {
			return varErr
		}
		switch input {
		case "a", "add", "var", "variable", "variables":
			variableKey, keyErr := e.readValue("Name: ")
			if keyErr != nil {
				return keyErr
			}
			variableValue, valErr := e.readValue("Value: ")
			if valErr != nil {
				return valErr
			}
			if v.Vars == nil {
				v.Vars = make(map[string]string)
			}
			if _, exists := v.Vars[variableKey]; exists {
				confirm, err := e.readValue(fmt.Sprintf("Variable '%s' already exists. Overwrite? (y/n): ", variableKey))
				if err != nil {
					return err
				}
				if confirm != "y" {
					break
				}
			}
			v.Vars[variableKey] = variableValue
		case "S", "show", "hide":
			e.showHidden = !e.showHidden
		case "D", "delete", "remove":
			variable, valErr := e.readValue("Variable name: ")
			if valErr != nil {
				return valErr
			}
			if _, exists := v.Vars[variable]; exists {
				delete(v.Vars, variable)
			} else {
				color.Red("Variable '%s' not found", variable)
			}
		case "b", "back":
			return nil
		case "q", "quit", "exit":
			var confirm string
			var err error
			confirm, err = e.readValue("Are you sure you wish to exit the vault? (y/n): ")
			if err == nil {
				if confirm == "y" {
					return ErrSaveAndExit
				}
			}
		case "?", "help":
			variableMenu()
		default:
			color.Red("Command not recognized")
		}
	}
	return nil
}

func (e *Edit) setDuration(v *vaulted.Vault) {
	var dur string
	var err error
	maxDuration := 999 * time.Hour
	if v.AWSKey != nil && v.AWSKey.ForgoTempCredGeneration == false {
		maxDuration = 36 * time.Hour
	}
	readMessage := fmt.Sprintf("Duration (15m–%s): ", formatDuration(maxDuration))
	dur, err = e.readValue(readMessage)
	if err == nil {
		duration, durErr := time.ParseDuration(dur)
		if durErr != nil {
			color.Red("%s", durErr)
			return
		}
		if duration < 15*time.Minute || duration > maxDuration {
			errorMessage := fmt.Sprintf("Duration must be between 15m and %s", formatDuration(maxDuration))
			color.Red(errorMessage)
			return
		}
		v.Duration = duration
	}
}

func formatDuration(duration time.Duration) string {
	dur := duration.String()
	if strings.HasSuffix(dur, "m0s") {
		dur = dur[:len(dur)-2]
	}
	if strings.HasSuffix(dur, "h0m") {
		dur = dur[:len(dur)-2]
	}
	return dur
}

func output(message string) {
	fmt.Printf("%s\n", message)
}

func (e *Edit) printVariables(v *vaulted.Vault) {
	color.Cyan("\nVariables:")
	if len(v.Vars) > 0 {
		var keys []string
		for key := range v.Vars {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			green.Printf("  %s: ", key)
			if !e.showHidden {
				fmt.Printf("%s\n", faintColor.Sprint("<hidden>"))
			} else {
				fmt.Printf("%s\n", v.Vars[key])
			}
		}
	} else {
		output("  [Empty]")
	}
}

func (e *Edit) printAWS(v *vaulted.Vault) {
	color.Cyan("\nAWS Key:")
	if v.AWSKey != nil {
		green.Printf("  Key ID: ")
		fmt.Printf("%s\n", v.AWSKey.ID)
		green.Printf("  Secret: ")
		if !e.showHidden {
			fmt.Printf("%s\n", faintColor.Sprint("<hidden>"))
		} else {
			fmt.Printf("%s\n", v.AWSKey.Secret)
		}
		green.Printf("  MFA: ")
		if v.AWSKey.MFA == "" {
			var warning string
			if !v.AWSKey.ForgoTempCredGeneration {
				warning = warningColor.Sprint(" (warning: some APIs will not function without MFA (e.g. IAM))")
			}
			fmt.Printf("%s %s\n", faintColor.Sprint("<not configured>"), warning)
		} else {
			fmt.Printf("%s\n", v.AWSKey.MFA)
		}
		if v.AWSKey.Role != "" {
			green.Printf("  Role: ")
			fmt.Printf("%s\n", v.AWSKey.Role)
		}
		green.Printf("  Substitute with temporary credentials: ")
		fmt.Printf("%t\n", !v.AWSKey.ForgoTempCredGeneration)
	} else {
		output("  [Empty]")
	}
}

func (e *Edit) printSSHKeys(v *vaulted.Vault) {
	color.Cyan("\nSSH Keys:")
	if len(v.SSHKeys) > 0 {
		keys := []string{}
		for key := range v.SSHKeys {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			green.Printf("  %s\n", key)
		}
	} else {
		output("  [Empty]")
	}
}

func (e *Edit) printDuration(v *vaulted.Vault) {
	cyan.Println("\nEnvironment:")
	green.Print("  Duration: ")
	var duration time.Duration
	if v.Duration == 0 {
		duration = vaulted.STSDurationDefault
	} else {
		duration = v.Duration
	}
	fmt.Printf("%s\n", formatDuration(duration))
}

func (e *Edit) readMenu(message string) (string, error) {
	if e.rlMenu == nil {
		var err error
		e.rlMenu, err = readline.New("")
		if err != nil {
			return "", err
		}
	}

	output("")
	input, err := e.readInput(menuColor.Sprint(message), e.rlMenu)
	output("")
	return input, err
}

func (e *Edit) readValue(message string) (string, error) {
	if e.rlValue == nil {
		var err error
		e.rlValue, err = readline.New("")
		if err != nil {
			return "", err
		}
	}
	return e.readInput(color.GreenString(message), e.rlValue)
}

func (e *Edit) readPrompt(message string) (string, error) {
	if e.rlValue == nil {
		var err error
		e.rlValue, err = readline.New("")
		if err != nil {
			return "", err
		}
	}
	return e.readInput(warningColor.Sprint(message), e.rlValue)
}

func (e *Edit) readInput(message string, rl *readline.Instance) (string, error) {
	rl.SetPrompt(message)
	line, err := rl.Readline()
	if err == readline.ErrInterrupt {
		return "", ErrAbort
	}
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(line), nil
}
