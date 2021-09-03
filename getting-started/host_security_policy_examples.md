# Security Policy Examples

Here, we demonstrate how to define host security policies.

* Process Execution Restriction
  * Block a specific executable \([hsp-kubearmor-dev-proc-path-block.yaml](https://github.com/kubearmor/KubeArmor/tree/master/examples/host-security-policies/hsp-kubearmor-dev-proc-path-block.yaml)\)

    ```text
    apiVersion: security.kubearmor.com/v1
    kind: KubeArmorHostPolicy
    metadata:
      name: hsp-kubearmor-dev-proc-path-block
    spec:
      nodeSelector:
        matchLabels:
          kubernetes.io/hostname: kubearmor-dev
      process:
        matchPaths:
        - path: /usr/bin/sleep # try sleep 1
      action:
        Block
    ```

    * Explanation: The purpose of this policy is to block the execution of '/bin/sleep' in a host whose host name is 'kubearmor-dev'. For this, we define 'kubernetes.io/hostname: kubearmor-dev' in nodeSelector -&gt; matchLabels and the specific path \('/bin/sleep'\) in process -&gt; matchPaths. Also, we put 'Block' as the action of this policy.

    * Verification: After applying this policy, please open a new terminal (or connect to the host with a new session) and run '/bin/sleep'. You will see that /bin/sleep is blocked. 

* File Access Restriction
  * Audit a critical file access \([hsp-kubearmor-dev-file-path-audit.yaml](https://github.com/kubearmor/KubeArmor/tree/master/examples/multiubuntu/security-policies/hsp-kubearmor-dev-file-path-audit.yaml)\)

    ```text
    apiVersion: security.kubearmor.com/v1
    kind: KubeArmorHostPolicy
    metadata:
      name: hsp-kubearmor-dev-file-path-audit
    spec:
      nodeSelector:
        matchLabels:
          kubernetes.io/hostname: kubearmor-dev
      file:
        matchPaths:
        - path: /etc/shadow # cat /etc/shadow
      action:
        Audit
    ```

    * Explanation: The purpose of this policy is to audit any file accesses to a critical file (i.e., '/etc/shadow'). Since we want to audit one critical file, we use matchPaths to specify the path of '/etc/shadow'.

    * Verification: After applying this policy, please open a new terminal (or connect to the host with a new session) and run 'sudo cat /etc/shadow'. Then, check the alert logs of KubeArmor.
