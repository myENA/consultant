package consultant

import "github.com/hashicorp/consul/api"

type SiblingWatcherCallback func(index uint64, siblings map[string]*api.ServiceEntry)

type ServiceSiblings struct {

}


