// Copyright 2021 Authors of KubeArmor
// SPDX-License-Identifier: Apache-2.0

package enforcer

import (
	"bufio"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	kl "github.com/kubearmor/KubeArmor/KubeArmor/common"
	fd "github.com/kubearmor/KubeArmor/KubeArmor/feeder"
	tp "github.com/kubearmor/KubeArmor/KubeArmor/types"
)

// ====================== //
// == SELinux Enforcer == //
// ====================== //

// SELinuxEnforcer Structure
type SELinuxEnforcer struct {
	// logs
	Logger *fd.Feeder

	SELinuxProfiles     map[string]int
	SELinuxProfilesLock *sync.Mutex

	SELinuxContextTemplates string
}

// NewSELinuxEnforcer Function
func NewSELinuxEnforcer(logger *fd.Feeder) *SELinuxEnforcer {
	se := &SELinuxEnforcer{}

	se.Logger = logger

	se.SELinuxProfiles = map[string]int{}
	se.SELinuxProfilesLock = &sync.Mutex{}

	if _, err := os.Stat("/usr/sbin/semanage"); err != nil {
		se.Logger.Errf("Failed to find /usr/sbin/semanage (%s)", err.Error())
		return nil
	}

	se.SELinuxContextTemplates = "/KubeArmor/templates/"

	if kl.IsK8sLocal() {
		if ex, err := os.Executable(); err == nil {
			se.SELinuxContextTemplates = filepath.Dir(ex) + "/templates/"
		}
	}

	// install template cil
	if err := kl.RunCommandAndWaitWithErr("semanage", []string{"module", "-a", se.SELinuxContextTemplates + "base_container.cil"}); err != nil {
		se.Logger.Printf("Failed to register a SELinux profile, %s (%s)", se.SELinuxContextTemplates+"base_container.cil", err.Error())
		return nil
	}

	return se
}

// DestroySELinuxEnforcer Function
func (se *SELinuxEnforcer) DestroySELinuxEnforcer() error {
	for profileName := range se.SELinuxProfiles {
		emptyPod := tp.K8sPod{Metadata: map[string]string{}}
		se.UnregisterSELinuxProfile(emptyPod, profileName)
	}

	// remove template cil
	if err := kl.RunCommandAndWaitWithErr("semanage", []string{"module", "-r", "base_container"}); err != nil {
		se.Logger.Printf("Failed to register a SELinux profile, %s (%s)", se.SELinuxContextTemplates+"base_container.cil", err.Error())
		return nil
	}

	return nil
}

// ================================ //
// == SELinux Profile Management == //
// ================================ //

// SELinux Flags
const (
	SELinuxDirReadOnly   = "getattr search open read lock ioctl"
	SELinuxDirReadWrite  = "getattr search open read lock ioctl setattr write link add_name remove_name reparent lock create unlink rename rmdir"
	SELinuxFileReadOnly  = "getattr ioctl lock open read"
	SELinuxFileReadWrite = "getattr ioctl lock open read write append lock create rename link unlink"
)

// RegisterSELinuxProfile Function
func (se *SELinuxEnforcer) RegisterSELinuxProfile(pod tp.K8sPod, containerName, profileName string) bool {
	namespace := pod.Metadata["namespaceName"]
	podName := pod.Metadata["podName"]

	defaultProfile := "(block " + profileName + "\n" +
		"	(blockinherit container)\n" +
		// "	(blockinherit restricted_net_container)\n" +
		"	(allow process process (capability (dac_override)))\n"

	for _, hostVolume := range pod.HostVolumes {
		if readOnly, ok := hostVolume.UsedByContainerReadOnly[containerName]; ok {
			context, err := kl.GetSELinuxType(hostVolume.PathName)
			if err != nil {
				se.Logger.Errf("Failed to get the SELinux type of %s (%s)", hostVolume.PathName, err.Error())
				return false
			}

			contextLine := "	(allow process " + context

			if readOnly {
				if hostVolume.Type == "Directory" {
					contextDirLine := contextLine + " (dir (" + SELinuxDirReadOnly + ")))\n"
					defaultProfile = defaultProfile + contextDirLine
				} else {
					contextFileLine := contextLine + " (file (" + SELinuxFileReadOnly + ")))\n"
					defaultProfile = defaultProfile + contextFileLine
				}
			} else {
				if hostVolume.Type == "Directory" {
					contextDirLine := contextLine + " (dir (" + SELinuxDirReadWrite + ")))\n"
					defaultProfile = defaultProfile + contextDirLine
				} else {
					contextFileLine := contextLine + " (file (" + SELinuxFileReadWrite + ")))\n"
					defaultProfile = defaultProfile + contextFileLine
				}
			}
		}
	}
	defaultProfile = defaultProfile + ")\n"

	se.SELinuxProfilesLock.Lock()
	defer se.SELinuxProfilesLock.Unlock()

	profilePath := se.SELinuxContextTemplates + profileName + ".cil"
	if _, err := os.Stat(filepath.Clean(profilePath)); err == nil {
		if err := os.Remove(filepath.Clean(profilePath)); err != nil {
			se.Logger.Err(err.Error())
			return false
		}
	}

	newFile, err := os.Create(filepath.Clean(profilePath))
	if err != nil {
		se.Logger.Err(err.Error())
		return false
	}
	if _, err := newFile.WriteString(defaultProfile); err != nil {
		se.Logger.Err(err.Error())
		return false
	}
	if err := newFile.Close(); err != nil {
		se.Logger.Err(err.Error())
	}

	if err := kl.RunCommandAndWaitWithErr("semanage", []string{"module", "-a", profilePath}); err != nil {
		se.Logger.Printf("Failed to register a SELinux profile, %s for (%s/%s) (%s)", profileName, namespace, podName, err.Error())
		return false
	}

	if _, ok := se.SELinuxProfiles[profileName]; !ok {
		se.SELinuxProfiles[profileName] = 1
		se.Logger.Printf("Registered a SELinux profile (%s) for (%s/%s)", profileName, namespace, podName)
		return true
	}

	return false
}

// UnregisterSELinuxProfile Function
func (se *SELinuxEnforcer) UnregisterSELinuxProfile(pod tp.K8sPod, profileName string) bool {
	namespace := pod.Metadata["namespaceName"]
	podName := pod.Metadata["podName"]

	se.SELinuxProfilesLock.Lock()
	defer se.SELinuxProfilesLock.Unlock()

	profilePath := se.SELinuxContextTemplates + profileName + ".cil"

	if _, err := os.Stat(filepath.Clean(profilePath)); err != nil {
		se.Logger.Printf("Unabale to unregister a SELinux profile (%s) for (%s/%s) (%s)", profileName, namespace, podName, err.Error())
		return false
	}

	if _, err := ioutil.ReadFile(filepath.Clean(profilePath)); err != nil {
		if namespace == "" || podName == "" {
			se.Logger.Printf("Unabale to unregister a SELinux profile (%s) (%s)", profileName, err.Error())
		} else {
			se.Logger.Printf("Unabale to unregister a SELinux profile (%s) for (%s/%s) (%s)", profileName, namespace, podName, err.Error())
		}
		return false
	}

	referenceCount, ok := se.SELinuxProfiles[profileName]

	if !ok {
		if namespace == "" || podName == "" {
			se.Logger.Printf("Failed to unregister an unknown SELinux profile (%s) (not exist profile in the enforecer)", profileName)
		} else {
			se.Logger.Printf("Failed to unregister an unknown SELinux profile (%s) for (%s/%s) (not exist profile in the enforecer)", profileName, namespace, podName)
		}
		return false
	}

	if referenceCount > 1 {
		se.SELinuxProfiles[profileName]--
		se.Logger.Printf("Decreased the refCount (%d -> %d) of a SELinux profile (%s)", se.SELinuxProfiles[profileName]+1, se.SELinuxProfiles[profileName], profileName)
	} else {
		if err := kl.RunCommandAndWaitWithErr("semanage", []string{"module", "-r", profileName}); err != nil {
			if namespace == "" || podName == "" {
				se.Logger.Printf("Unabale to unregister a SELinux profile, %s (%s)", profileName, err.Error())
			} else {
				se.Logger.Printf("Unabale to unregister a SELinux profile, %s for (%s/%s) (%s)", profileName, namespace, podName, err.Error())
			}
			return false
		}

		if err := os.Remove(filepath.Clean(profilePath)); err != nil {
			se.Logger.Errf("Failed to remove %s (%s)", profilePath, err.Error())
			return false
		}

		delete(se.SELinuxProfiles, profileName)

		if namespace == "" || podName == "" {
			se.Logger.Printf("Unregistered a SELinux profile (%s)", profileName)
		} else {
			se.Logger.Printf("Unregistered a SELinux profile (%s) for (%s/%s)", profileName, namespace, podName)
		}
	}

	return true
}

// ================================= //
// == Security Policy Enforcement == //
// ================================= //

// GenerateSELinuxProfile Function
func (se *SELinuxEnforcer) GenerateSELinuxProfile(endPoint tp.EndPoint, profileName string, securityPolicies []tp.SecurityPolicy) (int, string, bool) {
	securityRules := 0

	if _, err := os.Stat(filepath.Clean(se.SELinuxContextTemplates + profileName + ".cil")); os.IsNotExist(err) {
		return 0, err.Error(), false
	}

	file, err := os.Open(filepath.Clean(se.SELinuxContextTemplates + profileName + ".cil"))
	if err != nil {
		return 0, err.Error(), false
	}

	oldProfile := ""

	fscanner := bufio.NewScanner(file)
	for fscanner.Scan() {
		line := fscanner.Text()
		oldProfile += (line + "\n")
	}
	if err := file.Close(); err != nil {
		se.Logger.Err(err.Error())
	}

	// key: container-side path, val: host-side path
	mountedPathToHostPath := map[string]string{}

	// write default volume
	newProfile := "(block " + profileName + "\n" +
		"	(blockinherit container)\n" +
		// "	(blockinherit restricted_net_container)\n" +
		"	(allow process process (capability (dac_override)))\n"

	found := false

	for _, hostVolume := range endPoint.HostVolumes {
		for containerName := range hostVolume.UsedByContainerPath {
			if !strings.Contains(profileName, containerName) {
				continue
			}

			found = true

			if readOnly, ok := hostVolume.UsedByContainerReadOnly[containerName]; ok {
				mountedPathToHostPath[hostVolume.UsedByContainerPath[containerName]] = hostVolume.PathName

				context, err := kl.GetSELinuxType(hostVolume.PathName)
				if err != nil {
					se.Logger.Errf("Failed to get the SELinux type of %s (%s)", hostVolume.PathName, err.Error())
					return 0, "", false
				}

				contextLine := "	(allow process " + context

				if readOnly {
					contextDirLine := contextLine + " (dir (" + SELinuxDirReadOnly + ")))\n"
					contextFileLine := contextLine + " (file (" + SELinuxFileReadOnly + ")))\n"
					newProfile = newProfile + contextDirLine + contextFileLine
				} else {
					contextDirLine := contextLine + " (dir (" + SELinuxDirReadWrite + ")))\n"
					contextFileLine := contextLine + " (file (" + SELinuxFileReadWrite + ")))\n"
					newProfile = newProfile + contextDirLine + contextFileLine
				}
			}
		}

		if !found {
			return 0, "", false
		}

		// write policy volume
		for _, policy := range securityPolicies {
			for _, vol := range policy.Spec.SELinux.MatchVolumeMounts {
				// file
				if len(vol.Path) > 0 {
					absolutePath := vol.Path
					readOnly := vol.ReadOnly

					for containerPath, hostPath := range mountedPathToHostPath {
						if strings.Contains(absolutePath, containerPath) {
							filePath := strings.Split(absolutePath, containerPath)[1]
							hostAbsolutePath := hostPath + filePath

							if context, err := kl.GetSELinuxType(hostAbsolutePath); err != nil {
								se.Logger.Errf("Failed to get the SELinux type of %s (%s)", hostVolume.PathName, err.Error())
								break
							} else {
								contextLine := "	(allow process " + context

								if readOnly {
									contextFileLine := contextLine + " (file (" + SELinuxFileReadOnly + ")))\n"
									newProfile = newProfile + contextFileLine
									securityRules++
								} else {
									contextFileLine := contextLine + " (file (" + SELinuxFileReadWrite + ")))\n"
									newProfile = newProfile + contextFileLine
									securityRules++
								}
							}
						}
					}
				}

				// directory
				if len(vol.Directory) > 0 {
					absolutePath := vol.Directory
					readOnly := vol.ReadOnly

					for containerPath, hostPath := range mountedPathToHostPath {
						if strings.Contains(absolutePath, containerPath) {
							filePath := strings.Split(absolutePath, containerPath)[1]
							hostAbsolutePath := hostPath + filePath

							if context, err := kl.GetSELinuxType(hostAbsolutePath); err != nil {
								se.Logger.Errf("Failed to get the SELinux type of %s (%s)", hostVolume.PathName, err.Error())
								break
							} else {
								contextLine := "	(allow process " + context

								if readOnly {
									contextDirLine := contextLine + " (dir (" + SELinuxDirReadOnly + ")))\n"
									newProfile = newProfile + contextDirLine
									securityRules++
								} else {
									contextDirLine := contextLine + " (dir (" + SELinuxDirReadWrite + ")))\n"
									newProfile = newProfile + contextDirLine
									securityRules++
								}
							}
						}
					}
				}
			}
		}
	}

	newProfile = newProfile + ")\n"

	if newProfile != oldProfile {
		return securityRules, newProfile, true
	}

	return 0, "", false
}

// UpdateSELinuxProfile Function
func (se *SELinuxEnforcer) UpdateSELinuxProfile(endPoint tp.EndPoint, seLinuxProfile string, securityPolicies []tp.SecurityPolicy) {
	if ruleCount, newProfile, ok := se.GenerateSELinuxProfile(endPoint, seLinuxProfile, securityPolicies); ok {
		newfile, err := os.Create(filepath.Clean(se.SELinuxContextTemplates + seLinuxProfile + ".cil"))
		if err != nil {
			se.Logger.Err(err.Error())
			return
		}
		defer func() {
			if err := newfile.Close(); err != nil {
				se.Logger.Err(err.Error())
			}
		}()

		if _, err := newfile.WriteString(newProfile); err != nil {
			se.Logger.Err(err.Error())
			return
		}

		if err := newfile.Sync(); err != nil {
			se.Logger.Err(err.Error())
			return
		}

		if err := kl.RunCommandAndWaitWithErr("semanage", []string{"module", "-a", se.SELinuxContextTemplates + seLinuxProfile + ".cil"}); err == nil {
			se.Logger.Printf("Updated %d security rule(s) to %s/%s/%s", ruleCount, endPoint.NamespaceName, endPoint.EndPointName, seLinuxProfile)
		} else {
			se.Logger.Printf("Failed to update %d security rule(s) to %s/%s/%s (%s)", ruleCount, endPoint.NamespaceName, endPoint.EndPointName, seLinuxProfile, err.Error())
		}
	}
}

// UpdateSecurityPolicies Function
func (se *SELinuxEnforcer) UpdateSecurityPolicies(endPoint tp.EndPoint) {
	selinuxProfiles := []string{}

	for _, seLinuxProfile := range endPoint.SELinuxProfiles {
		if !kl.ContainsElement(selinuxProfiles, seLinuxProfile) {
			selinuxProfiles = append(selinuxProfiles, seLinuxProfile)
		}
	}

	for _, selinuxProfile := range selinuxProfiles {
		se.UpdateSELinuxProfile(endPoint, selinuxProfile, endPoint.SecurityPolicies)
	}
}

// ====================================== //
// == Host Security Policy Enforcement == //
// ====================================== //

// UpdateHostSecurityPolicies Function
func (se *SELinuxEnforcer) UpdateHostSecurityPolicies(secPolicies []tp.HostSecurityPolicy) {
	//
}
