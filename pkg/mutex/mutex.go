package mutex

import "sync"

type KeyedMutex struct {
	muMap sync.Map
}

func (km *KeyedMutex) Lock(key string) {
	mu, _ := km.muMap.LoadOrStore(key, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
}

func (km *KeyedMutex) Unlock(key string) {
	mu, ok := km.muMap.Load(key)
	if ok {
		mu.(*sync.Mutex).Unlock()
		km.muMap.Delete(key)
	}
}
