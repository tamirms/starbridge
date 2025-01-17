package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	supportlog "github.com/stellar/go/support/log"
	"github.com/stellar/starbridge/p2p"
)

type CollectorConfig struct {
	Logger *supportlog.Entry
	PubSub *pubsub.PubSub
	Store  *Store
}

type Collector struct {
	logger       *supportlog.Entry
	pubSub       *pubsub.PubSub
	listenTopic  *pubsub.Topic
	publishTopic *pubsub.Topic
	store        *Store
}

func NewCollector(config CollectorConfig) (*Collector, error) {
	listenTopic, err := config.PubSub.Join("starbridge-stellar-transactions-signed")
	if err != nil {
		return nil, err
	}
	publishTopic, err := config.PubSub.Join("starbridge-stellar-transactions-signed-aggregated")
	if err != nil {
		return nil, err
	}
	c := &Collector{
		logger:       config.Logger,
		store:        config.Store,
		pubSub:       config.PubSub,
		listenTopic:  listenTopic,
		publishTopic: publishTopic,
	}
	return c, nil
}

func (c *Collector) Collect() error {
	sub, err := c.listenTopic.Subscribe()
	if err != nil {
		return err
	}
	logger := c.logger.WithField("topic", c.listenTopic.String())
	logger.Info("Subscribed")
	ctx := context.Background()
	for {
		logger := logger

		raw, err := sub.Next(ctx)
		if err != nil {
			return err
		}

		hash := sha256.Sum256(raw.Data)
		hashHex := hex.EncodeToString(hash[:])
		logger = logger.WithField("msghash", hashHex)

		logger.Infof("Msg received")

		msg := p2p.Message{}
		err = msg.UnmarshalBinary(raw.Data)
		if err != nil {
			logger.Errorf("Unmarshaling message: %s", err)
			continue
		}

		if msg.V != 0 {
			logger.Errorf("Dropping message with unsupported version %d", msg.V)
			continue
		}

		logger = logger.WithField("msgbodysize", len(msg.V0.Body))
		logger = logger.WithField("msgsigcount", len(msg.V0.Signatures))

		bodyHash := sha256.Sum256(msg.V0.Body)
		bodyHashHex := hex.EncodeToString(bodyHash[:])
		logger = logger.WithField("msgbodyhash", bodyHashHex)

		logger.Infof("Msg unpacked")

		msg.V0, err = c.store.StoreAndUpdate(msg.V0)
		if err != nil {
			return err
		}
		logger = logger.WithField("sigcount", len(msg.V0.Signatures))
		logger.Infof("Msg updated from store")

		msgBytes, err := msg.MarshalBinary()
		if err != nil {
			return err
		}

		logger = logger.WithField("topic", c.publishTopic.String())
		err = c.publishTopic.Publish(ctx, msgBytes)
		if err != nil {
			return fmt.Errorf("publishing msg: %w", err)
		}
		logger.Infof("Msg published")
	}
}
