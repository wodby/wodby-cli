package api

const (
	APP_BUILD = `
	query appBuild($id: Int!) {
		appBuild(id: $id) {
			id
			number
			gitRefType
			gitRef
			config {
				registryHost
				registryRepository
				services {
					name
					title
					managed
					main
					image
					dockerfile
					dockerignore
				}
			}
		}
	}`

	DOCKER_REGISTRY_CREDENTIALS = `
	query dockerRegistryCredentials($appBuildID: Int!) {
		dockerRegistryCredentials(appBuildID: $appBuildID) {
			username
			password
		}
	}`

	DEPLOY = `
	mutation deployFromCI($input: DeploymentFromCIInput!) {
		deployFromCI(input: $input) {
			id
		}
	}`
)
