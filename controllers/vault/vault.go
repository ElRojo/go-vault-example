package vault

import (
	"context"
	"fmt"
	"slices"

	"github.com/rs/zerolog/log"

	"time"

	"github.com/hashicorp/vault-client-go"
	"github.com/hashicorp/vault-client-go/schema"
)

type Vaulter interface {
	makeEngineSlice(ctx context.Context, client *vault.Client) ([]string, error)
	createEngines(ctx context.Context, client *vault.Client, secret *Secret) (string, error)
	GetSecretEngine(ctx context.Context, client *vault.Client) map[string]interface{}
	hydrateNewSecretsStruct(ctx context.Context, c *vault.Client, s []*Secret, secretMap map[string]secretMap)
	InitVaultClient(token string, url string) (context.Context, *vault.Client, error)
	writeSecret(ctx context.Context, client *vault.Client, path string, data map[string]interface{}) error
}

func (v *AcmeVault) makeEngineSlice(ctx context.Context, client *vault.Client) ([]string, error) {
	engineSlice := []string{}
	for eng := range v.GetSecretEngine(ctx, client) {
		engineSlice = append(engineSlice, eng)
	}
	return engineSlice, nil
}

func (v *AcmeVault) createEngines(ctx context.Context, client *vault.Client, secret *Secret) (string, error) {
	_, err := client.System.MountsEnableSecretsEngine(ctx, secret.Engine, schema.MountsEnableSecretsEngineRequest{Type: "kv-v2"})
	if err != nil {
		log.Warn().Err(err).Msg("err in createEngines")
		return "", err
	}
	return "Processed: " + secret.Engine, nil
}

func (v *AcmeVault) GetSecretEngine(ctx context.Context, client *vault.Client) map[string]interface{} {
	engs, err := client.System.MountsListSecretsEngines(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}
	return engs.Data
}

func (v *AcmeVault) writeSecret(ctx context.Context, client *vault.Client, path string, data map[string]interface{}) error {
	if _, err := client.Write(ctx, path, map[string]interface{}{"data": data}); err != nil {
		log.Fatal().Err(err).Msg("")
		return err
	}
	return nil
}

func ReadSecret(ctx context.Context, c *vault.Client, path string, secret string) string {
	response, err := c.Read(ctx, path)
	if err != nil {
		return "error reading secret"
	}
	data, ok := response.Data["data"].(map[string]interface{})
	if !ok {
		return fmt.Sprintf("response data for %s secret not ok", secret)
	}
	return data[secret].(string)
}

func CreateDataInVault(ctx context.Context, client *vault.Client, v Vaulter, s []*Secret) error {
	engines, err := v.makeEngineSlice(ctx, client)
	if err != nil {
		return err
	}

	for _, secret := range s {
		if !slices.Contains(engines, secret.Engine+"/") {
			log.Warn().Msg("trying to create" + secret.Engine)
			eng, err := v.createEngines(ctx, client, secret)
			if err != nil {
				log.Warn().Err(err)
			}
			log.Info().Msg("created: " + eng)
		}
		for _, kv := range secret.Keys {
			path := fmt.Sprintf("%v/data/%v", secret.Engine, kv.Path)
			if err := v.writeSecret(ctx, client, path, kv.Data); err != nil {
				log.Warn().Err(err)
			} else {
				log.Info().Msgf("Secrets in: %q written", path)
			}
		}
	}
	return nil
}

func (v *AcmeVault) hydrateNewSecretsStruct(ctx context.Context, c *vault.Client, s []*Secret, secretMap map[string]secretMap) {
	for _, secret := range s {
		for _, kv := range secret.Keys {
			for key := range kv.Data {
				sm := secretMap[key]
				if sm.path != "" {
					value := ReadSecret(ctx, c, sm.path, sm.secret)
					kv.Data[key] = value
				}
			}
		}
	}
}

func (s *AcmeVault) InitVaultClient(token string, url string) (context.Context, *vault.Client, error) {
	var ctx = context.Background()

	client, err := vault.New(
		vault.WithAddress(url),
		vault.WithRequestTimeout(10*time.Second),
	)
	if err != nil {
		log.Fatal().Err(err)
		return nil, nil, err
	}
	err = client.SetToken(token)
	if err != nil {
		log.Fatal().Err(err)
		return nil, nil, err
	}

	return ctx, client, nil
}

func InitVault(ctx context.Context, client *vault.Client, v Vaulter, s []*Secret, c VaultConfig) (string, error) {
	if !c.Legacy && c.Copy {
		sm := initSecretMap()
		v.hydrateNewSecretsStruct(ctx, client, s, sm)
	}

	err := CreateDataInVault(ctx, client, v, s)
	if err != nil {
		return "", err
	}
	return "Vault complete", nil

}
