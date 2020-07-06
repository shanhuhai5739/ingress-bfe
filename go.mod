module github.com/baidu/ingress-bfe

go 1.13

require (
	github.com/eapache/channels v1.1.0
	github.com/eapache/queue v1.1.0 // indirect
	github.com/fullsailor/pkcs7 v0.0.0-20190404230743-d7302db945fa // indirect
	github.com/imdario/mergo v0.3.9 // indirect
	github.com/pkg/errors v0.9.1
	github.com/zakjan/cert-chain-resolver v0.0.0-20200409100953-fa92b0b5236f
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e // indirect
	k8s.io/api v0.18.5
	k8s.io/apimachinery v0.18.5
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/klog v1.0.0
	k8s.io/utils v0.0.0-20200619165400-6e3d28b6ed19 // indirect
)

replace k8s.io/client-go v11.0.0+incompatible => k8s.io/client-go v0.18.5
