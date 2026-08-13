package wodby1

import (
	"fmt"
	"sort"
	"strings"
)

const targetMailDeliveryLinkName = "sendmail"

type preparedMailDeliveryLink struct {
	instanceIndex     int
	instanceName      string
	serviceName       string
	linkedServiceName string
}

// prepareMailDeliveryLinks preserves the Wodby 1 mail_service selection. One
// shared selection belongs on the app's dedicated stack; differing selections
// are represented by app-service link overrides on each target instance.
func prepareMailDeliveryLinks(app *PreparedAppMigration) []ReviewItem {
	if app == nil {
		return nil
	}
	findings := []ReviewItem{}
	links := []preparedMailDeliveryLink{}
	for instanceIndex := range app.Instances {
		instance := &app.Instances[instanceIndex]
		sourceMail, found, err := selectedSourceMailService(instance.Source)
		if err != nil {
			findings = append(findings, ReviewItem{
				Severity: SeverityBlocking, App: app.App.App.Name, Instance: instance.Source.Name,
				Subject: "mail delivery", Message: err.Error(),
			})
			continue
		}
		if !found {
			continue
		}
		mailMapping, ok := instance.Services[sourceMail]
		if !ok || strings.TrimSpace(mailMapping.Target.StackService.Name) == "" {
			findings = append(findings, ReviewItem{
				Severity: SeverityBlocking, App: app.App.App.Name, Instance: instance.Source.Name,
				Subject: "mail delivery", Message: fmt.Sprintf("selected Wodby 1 mail service %q has no approved Wodby 2 mapping", sourceMail),
			})
			continue
		}
		targetMail := mailMapping.Target.StackService.Name
		if !instance.EffectiveState[targetMail] {
			findings = append(findings, ReviewItem{
				Severity: SeverityBlocking, App: app.App.App.Name, Instance: instance.Source.Name,
				Subject: "mail delivery", Message: fmt.Sprintf("selected target mail service %q is disabled", targetMail),
			})
			continue
		}
		linkSources := targetSendmailLinkServices(*instance)
		if len(linkSources) == 0 {
			findings = append(findings, ReviewItem{
				Severity: SeverityBlocking, App: app.App.App.Name, Instance: instance.Source.Name,
				Subject: "mail delivery", Message: "no mapped target application service exposes the Wodby 2 sendmail link",
			})
			continue
		}
		for _, serviceName := range linkSources {
			links = append(links, preparedMailDeliveryLink{
				instanceIndex: instanceIndex, instanceName: instance.Source.Name,
				serviceName: serviceName, linkedServiceName: targetMail,
			})
		}
	}

	byService := map[string][]preparedMailDeliveryLink{}
	for _, link := range links {
		byService[link.serviceName] = append(byService[link.serviceName], link)
	}
	serviceNames := make([]string, 0, len(byService))
	for serviceName := range byService {
		serviceNames = append(serviceNames, serviceName)
	}
	sort.Strings(serviceNames)
	for _, serviceName := range serviceNames {
		items := byService[serviceName]
		sharedTarget := items[0].linkedServiceName
		stackWide := len(items) == len(app.Instances)
		for _, item := range items[1:] {
			if item.linkedServiceName != sharedTarget {
				stackWide = false
				break
			}
		}
		if stackWide {
			configuration := app.StackConfiguration.Services[serviceName]
			configuration.Links = append(configuration.Links, PreparedStackServiceLink{
				Name: targetMailDeliveryLinkName, LinkedServiceName: sharedTarget,
			})
			sortPreparedStackServiceConfiguration(&configuration)
			app.StackConfiguration.Services[serviceName] = configuration
			findings = append(findings, ReviewItem{
				Severity: SeverityMigration, App: app.App.App.Name, Subject: "mail delivery",
				Message: fmt.Sprintf("all app instances use Wodby 2 mail service %q; stack service %q link %q will be set stack-wide", sharedTarget, serviceName, targetMailDeliveryLinkName),
			})
			continue
		}
		for _, item := range items {
			instance := &app.Instances[item.instanceIndex]
			instance.ServiceLinks = append(instance.ServiceLinks, PreparedAppServiceLink{
				ServiceName: item.serviceName, Name: targetMailDeliveryLinkName,
				LinkedServiceName: item.linkedServiceName,
			})
			findings = append(findings, ReviewItem{
				Severity: SeverityMigration, App: app.App.App.Name, Instance: item.instanceName,
				Subject: "mail delivery",
				Message: fmt.Sprintf("target service %q link %q will use %q for this app instance", item.serviceName, targetMailDeliveryLinkName, item.linkedServiceName),
			})
		}
	}
	return findings
}

func selectedSourceMailService(instance Instance) (string, bool, error) {
	if raw, found := instance.Properties["mail_service"]; found {
		service, ok := raw.(string)
		if !ok {
			return "", false, fmt.Errorf("source mail_service must be a service name")
		}
		service = strings.ToLower(strings.TrimSpace(service))
		switch service {
		case "mailhog", "opensmtpd":
			if !sourceServiceEnabled(instance.Services, service) {
				return "", false, fmt.Errorf("selected Wodby 1 mail service %q is not enabled", service)
			}
			return service, true, nil
		case "":
			return "", false, nil
		default:
			return "", false, fmt.Errorf("unsupported Wodby 1 mail_service %q; expected opensmtpd or mailhog", service)
		}
	}
	if raw, found := instance.Properties["php_mail_catcher"]; found {
		enabled, ok := raw.(bool)
		if !ok {
			return "", false, fmt.Errorf("source php_mail_catcher must be a boolean")
		}
		service := "opensmtpd"
		if enabled {
			service = "mailhog"
		}
		if !sourceServiceEnabled(instance.Services, service) {
			return "", false, fmt.Errorf("legacy php_mail_catcher=%t selects Wodby 1 service %q, but it is not enabled", enabled, service)
		}
		return service, true, nil
	}
	// Old instances may predate the persisted setting. Match Wodby 1's own
	// initialization order: MailHog first, then OpenSMTPD.
	for _, service := range []string{"mailhog", "opensmtpd"} {
		if sourceServiceEnabled(instance.Services, service) {
			return service, true, nil
		}
	}
	return "", false, nil
}

func targetSendmailLinkServices(instance PreparedInstance) []string {
	services := map[string]bool{}
	managed := sourceStackFamily(instance.Source.Stack) != ""
	for _, mapping := range instance.Services {
		target := strings.TrimSpace(mapping.Target.StackService.Name)
		if target == "" || !instance.EffectiveState[target] {
			continue
		}
		hasLink := managed && target == "php"
		if manifest := mapping.Target.ServiceRevision.Manifest; manifest != nil {
			for _, link := range manifest.Links {
				if link.Name == targetMailDeliveryLinkName {
					hasLink = true
					break
				}
			}
		}
		if hasLink {
			services[target] = true
		}
	}
	result := make([]string, 0, len(services))
	for service := range services {
		result = append(result, service)
	}
	sort.Strings(result)
	return result
}
