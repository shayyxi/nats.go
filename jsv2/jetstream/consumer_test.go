// Copyright 2020-2022 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jetstream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestConsumerInfo(t *testing.T) {
	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	t.Run("get consumer info, ok", func(t *testing.T) {
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{
			Durable:     "cons",
			AckPolicy:   AckExplicitPolicy,
			Description: "test consumer",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		info, err := c.Info(ctx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if info.Stream != "foo" {
			t.Fatalf("Invalid stream name; expected: 'foo'; got: %s", info.Stream)
		}
		if info.Config.Description != "test consumer" {
			t.Fatalf("Invalid consumer description; expected: 'test consumer'; got: %s", info.Config.Description)
		}

		// update consumer and see if info is updated
		_, err = s.UpdateConsumer(ctx, ConsumerConfig{
			Durable:     "cons",
			AckPolicy:   AckExplicitPolicy,
			Description: "updated consumer",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		info, err = c.Info(ctx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if info.Stream != "foo" {
			t.Fatalf("Invalid stream name; expected: 'foo'; got: %s", info.Stream)
		}
		if info.Config.Description != "updated consumer" {
			t.Fatalf("Invalid consumer description; expected: 'updated consumer'; got: %s", info.Config.Description)
		}
	})

	t.Run("consumer does not exist", func(t *testing.T) {
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		s, err := js.Stream(ctx, "foo")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.Consumer(ctx, "cons")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if err := s.DeleteConsumer(ctx, "cons"); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		_, err = c.Info(ctx)
		if err == nil || !errors.Is(err, ErrConsumerNotFound) {
			t.Fatalf("Expected error: %v; got: %v", ErrConsumerNotFound, err)
		}
	})
}

func TestConsumerCachedInfo(t *testing.T) {
	srv := RunBasicJetStreamServer()
	defer shutdownJSServerAndRemoveStorage(t, srv)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	js, err := New(nc)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer nc.Close()

	s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	c, err := s.CreateConsumer(ctx, ConsumerConfig{
		Durable:     "cons",
		AckPolicy:   AckExplicitPolicy,
		Description: "test consumer",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	info := c.CachedInfo()

	if info.Stream != "foo" {
		t.Fatalf("Invalid stream name; expected: 'foo'; got: %s", info.Stream)
	}
	if info.Config.Description != "test consumer" {
		t.Fatalf("Invalid consumer description; expected: 'test consumer'; got: %s", info.Config.Description)
	}

	// update consumer and see if info is updated
	_, err = s.UpdateConsumer(ctx, ConsumerConfig{
		Durable:     "cons",
		AckPolicy:   AckExplicitPolicy,
		Description: "updated consumer",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	info = c.CachedInfo()

	if info.Stream != "foo" {
		t.Fatalf("Invalid stream name; expected: 'foo'; got: %s", info.Stream)
	}

	// description should not be updated when using cached values
	if info.Config.Description != "test consumer" {
		t.Fatalf("Invalid consumer description; expected: 'updated consumer'; got: %s", info.Config.Description)
	}

}

func TestPullConsumerFetch(t *testing.T) {
	testSubject := "FOO.123"
	testMsgs := []string{"m1", "m2", "m3", "m4", "m5"}
	publishTestMsgs := func(t *testing.T, nc *nats.Conn) {
		for _, msg := range testMsgs {
			if err := nc.Publish(testSubject, []byte(msg)); err != nil {
				t.Fatalf("Unexpected error during publish: %s", err)
			}
		}
	}

	t.Run("no options", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		publishTestMsgs(t, nc)
		msgs, err := c.Fetch(5)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		received := make([]Msg, 0)
		for i := 0; ; i++ {
			select {
			case msg := <-msgs.Messages():
				if msg == nil {
					if len(testMsgs) != len(received) {
						t.Fatalf("Invalid number of messages received; want: %d; got: %d", len(testMsgs), len(received))
					}
					return
				}
				if string(msg.Data()) != testMsgs[i] {
					t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
				}
				received = append(received, msg)
			case err := <-msgs.Error():
				t.Fatalf("Unexpected error during fetch: %v", err)
			}
		}
	})

	t.Run("no options, fetch single messages one by one", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		res := make([]Msg, 0)
		errs := make(chan error)
		done := make(chan struct{})
		go func() {
			for {
				if len(res) == len(testMsgs) {
					close(done)
					return
				}
				msgs, err := c.Fetch(1)
				if err != nil {
					errs <- err
					return
				}
				select {
				case msg := <-msgs.Messages():
					res = append(res, msg)
				case err := <-msgs.Error():
					errs <- err
					return
				}
			}
		}()

		time.Sleep(10 * time.Millisecond)
		publishTestMsgs(t, nc)
		select {
		case err := <-errs:
			t.Fatalf("Unexpected error: %v", err)
		case <-done:
			if len(res) != len(testMsgs) {
				t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(res))
			}
		}
		for i, msg := range res {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with no wait, no messages at the time of request", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		msgs, err := c.FetchNoWait(5)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		publishTestMsgs(t, nc)

		msg := <-msgs.Messages()
		if msg != nil {
			t.Fatalf("Expected no messages; got: %s", string(msg.Data()))
		}
	})

	t.Run("with no wait, some messages available", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		publishTestMsgs(t, nc)
		msgs, err := c.FetchNoWait(5)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		publishTestMsgs(t, nc)

		var msgsNum int
	Loop:
		for i := 0; ; i++ {
			select {
			case msg := <-msgs.Messages():
				if msg == nil {
					break Loop
				}
				msgsNum++
			case err := <-msgs.Error():
				t.Fatalf("Unexpected error during fetch: %v", err)
			}
		}

		if msgsNum != len(testMsgs) {
			t.Fatalf("Expected 5 messages, got: %d", msgsNum)
		}
	})

	t.Run("with active streaming", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = c.Consume(func(_ Msg, _ error) {})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		_, err = c.Fetch(5)
		if err == nil || !errors.Is(err, ErrConsumerHasActiveSubscription) {
			t.Fatalf("Expected error: %v; got: %v", ErrConsumerHasActiveSubscription, err)
		}

		_, err = c.FetchNoWait(5)
		if err == nil || !errors.Is(err, ErrConsumerHasActiveSubscription) {
			t.Fatalf("Expected error: %v; got: %v", ErrConsumerHasActiveSubscription, err)
		}
	})

	t.Run("with timeout", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		msgs, err := c.Fetch(5, WithFetchTimeout(50*time.Millisecond))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		msg := <-msgs.Messages()
		if msg != nil {
			t.Fatalf("Expected no messages; got: %s", string(msg.Data()))
		}
	})

	t.Run("with invalid timeout value", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = c.Fetch(5, WithFetchTimeout(-50*time.Millisecond))
		if !errors.Is(err, ErrInvalidOption) {
			t.Fatalf("Expected error: %v; got: %v", ErrInvalidOption, err)
		}
	})
}

func TestPullConsumerNext_WithCluster(t *testing.T) {
	testSubject := "FOO.123"
	testMsgs := []string{"m1", "m2", "m3", "m4", "m5"}
	publishTestMsgs := func(t *testing.T, nc *nats.Conn) {
		for _, msg := range testMsgs {
			if err := nc.Publish(testSubject, []byte(msg)); err != nil {
				t.Fatalf("Unexpected error during publish: %s", err)
			}
		}
	}

	name := "cluster"
	stream := StreamConfig{
		Name:     name,
		Replicas: 1,
		Subjects: []string{"FOO.*"},
	}
	t.Run("no options", func(t *testing.T) {
		withJSClusterAndStream(t, name, 3, stream, func(t *testing.T, subject string, srvs ...*jsServer) {
			srv := srvs[0]
			nc, err := nats.Connect(srv.ClientURL())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			js, err := New(nc)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer nc.Close()

			s, err := js.Stream(ctx, stream.Name)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			publishTestMsgs(t, nc)
			msgs, err := c.Fetch(5)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			received := make([]Msg, 0)
			for i := 0; ; i++ {
				select {
				case msg := <-msgs.Messages():
					if msg == nil {
						if len(testMsgs) != len(received) {
							t.Fatalf("Invalid number of messages received; want: %d; got: %d", len(testMsgs), len(received))
						}
						return
					}
					if string(msg.Data()) != testMsgs[i] {
						t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
					}
					received = append(received, msg)
				case err := <-msgs.Error():
					t.Fatalf("Unexpected error during fetch: %v", err)
				}
			}
		})
	})

	t.Run("with no wait, no messages at the time of request", func(t *testing.T) {
		withJSClusterAndStream(t, name, 3, stream, func(t *testing.T, subject string, srvs ...*jsServer) {
			nc, err := nats.Connect(srvs[0].ClientURL())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			js, err := New(nc)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer nc.Close()

			s, err := js.Stream(ctx, stream.Name)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			msgs, err := c.FetchNoWait(5)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			publishTestMsgs(t, nc)

			msg := <-msgs.Messages()
			if msg != nil {
				t.Fatalf("Expected no messages; got: %s", string(msg.Data()))
			}
		})
	})
}

func TestPullConsumerMessages(t *testing.T) {
	testSubject := "FOO.123"
	testMsgs := []string{"m1", "m2", "m3", "m4", "m5"}
	publishTestMsgs := func(t *testing.T, nc *nats.Conn) {
		for _, msg := range testMsgs {
			if err := nc.Publish(testSubject, []byte(msg)); err != nil {
				t.Fatalf("Unexpected error during publish: %s", err)
			}
		}
	}

	t.Run("no options", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		msgs := make([]Msg, 0)
		it, err := c.Messages()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		publishTestMsgs(t, nc)
		for i := 0; i < len(testMsgs); i++ {
			msg, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if msg == nil {
				break
			}
			msg.Ack()
			msgs = append(msgs, msg)

		}
		it.Stop()

		// calling Stop() multiple times should have no effect
		it.Stop()
		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
		_, err = it.Next()
		if err == nil || !errors.Is(err, ErrMsgIteratorClosed) {
			t.Fatalf("Expected error: %v; got: %v", ErrMsgIteratorClosed, err)
		}
	})

	t.Run("with custom batch size", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// subscribe to next request subject to verify how many next requests were sent
		sub, err := nc.SubscribeSync(fmt.Sprintf("$JS.API.CONSUMER.MSG.NEXT.foo.%s", c.CachedInfo().Name))
		if err != nil {
			t.Fatalf("Error on subscribe: %v", err)
		}

		msgs := make([]Msg, 0)
		it, err := c.Messages(WithMessagesBatchSize(2))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		publishTestMsgs(t, nc)
		for i := 0; i < len(testMsgs); i++ {
			msg, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if msg == nil {
				break
			}
			msg.Ack()
			msgs = append(msgs, msg)

		}
		it.Stop()
		time.Sleep(10 * time.Millisecond)
		requestsNum, _, err := sub.Pending()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// with batch size set to 2, and 5 messages published on subject, there should be a total of 3 requests sent
		if requestsNum != 3 {
			t.Fatalf("Unexpected number of requests sent; want 3; got %d", requestsNum)
		}

		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with max fitting 1 message", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// subscribe to next request subject to verify how many next requests were sent
		sub, err := nc.SubscribeSync(fmt.Sprintf("$JS.API.CONSUMER.MSG.NEXT.foo.%s", c.CachedInfo().Name))
		if err != nil {
			t.Fatalf("Error on subscribe: %v", err)
		}

		msgs := make([]Msg, 0)
		it, err := c.Messages(WithMessagesMaxBytes(60))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		publishTestMsgs(t, nc)
		for i := 0; i < len(testMsgs); i++ {
			msg, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if msg == nil {
				break
			}
			msg.Ack()
			msgs = append(msgs, msg)

		}
		it.Stop()
		time.Sleep(10 * time.Millisecond)
		requestsNum, _, err := sub.Pending()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// with batch size set to 1, and 5 messages published on subject, there should be a total of 5 requests sent
		if requestsNum != 5 {
			t.Fatalf("Unexpected number of requests sent; want 5; got %d", requestsNum)
		}

		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with custom max bytes", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// subscribe to next request subject to verify how many next requests were sent
		sub, err := nc.SubscribeSync(fmt.Sprintf("$JS.API.CONSUMER.MSG.NEXT.foo.%s", c.CachedInfo().Name))
		if err != nil {
			t.Fatalf("Error on subscribe: %v", err)
		}

		msgs := make([]Msg, 0)
		it, err := c.Messages(WithMessagesMaxBytes(140))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		publishTestMsgs(t, nc)
		for i := 0; i < len(testMsgs); i++ {
			msg, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if msg == nil {
				break
			}
			msg.Ack()
			msgs = append(msgs, msg)

		}
		it.Stop()
		time.Sleep(10 * time.Millisecond)
		requestsNum, _, err := sub.Pending()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// with batch size set to 1, and 5 messages published on subject, there should be a total of 5 requests sent
		if requestsNum != 3 {
			t.Fatalf("Unexpected number of requests sent; want 3; got %d", requestsNum)
		}

		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with batch size set to 1", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// subscribe to next request subject to verify how many next requests were sent
		sub, err := nc.SubscribeSync(fmt.Sprintf("$JS.API.CONSUMER.MSG.NEXT.foo.%s", c.CachedInfo().Name))
		if err != nil {
			t.Fatalf("Error on subscribe: %v", err)
		}

		msgs := make([]Msg, 0)
		it, err := c.Messages(WithMessagesBatchSize(1))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		publishTestMsgs(t, nc)
		for i := 0; i < len(testMsgs); i++ {
			msg, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if msg == nil {
				break
			}
			msg.Ack()
			msgs = append(msgs, msg)

		}
		it.Stop()
		time.Sleep(10 * time.Millisecond)
		requestsNum, _, err := sub.Pending()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// with batch size set to 1, and 5 messages published on subject, there should be a total of 5 requests sent
		if requestsNum != 5 {
			t.Fatalf("Unexpected number of requests sent; want 5; got %d", requestsNum)
		}

		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("attempt iteration with active subscription twice on the same consumer", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = c.Consume(func(msg Msg, err error) {})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = c.Messages()
		if err == nil || !errors.Is(err, ErrConsumerHasActiveSubscription) {
			t.Fatalf("Expected error: %v; got: %v", ErrConsumerHasActiveSubscription, err)
		}
	})

	t.Run("create iterator, stop, then create again", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		msgs := make([]Msg, 0)
		it, err := c.Messages()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		publishTestMsgs(t, nc)
		for i := 0; i < len(testMsgs); i++ {
			msg, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if msg == nil {
				break
			}
			msg.Ack()
			msgs = append(msgs, msg)

		}
		it.Stop()
		time.Sleep(10 * time.Millisecond)

		publishTestMsgs(t, nc)
		it, err = c.Messages()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		for i := 0; i < len(testMsgs); i++ {
			msg, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if msg == nil {
				break
			}
			msg.Ack()
			msgs = append(msgs, msg)

		}
		it.Stop()
		if len(msgs) != 2*len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		expectedMsgs := append(testMsgs, testMsgs...)
		for i, msg := range msgs {
			if string(msg.Data()) != expectedMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with invalid batch size", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = c.Messages(WithMessagesBatchSize(-1))
		if err == nil || !errors.Is(err, ErrInvalidOption) {
			t.Fatalf("Expected error: %v; got: %v", ErrInvalidOption, err)
		}
	})

	t.Run("with idle heartbeat", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		msgs := make([]Msg, 0)
		it, err := c.Messages(WithMessagesHeartbeat(10 * time.Millisecond))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		publishTestMsgs(t, nc)
		for i := 0; i < len(testMsgs); i++ {
			msg, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if msg == nil {
				break
			}
			msg.Ack()
			msgs = append(msgs, msg)

		}
		it.Stop()

		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with idle heartbeat, server shutdown", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		it, err := c.Messages(WithMessagesHeartbeat(10 * time.Millisecond))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		errs := make(chan error)
		go func() {
			_, err = it.Next()
			errs <- err
		}()
		time.Sleep(100 * time.Millisecond)

		shutdownJSServerAndRemoveStorage(t, srv)
		select {
		case err := <-errs:
			if !errors.Is(err, ErrNoHeartbeat) {
				t.Fatalf("Unexpected error: %v; expected: %v", err, ErrNoHeartbeat)
			}
		case <-ctx.Done():
			t.Fatalf("Expected no heartbeat error: %s", ctx.Err())
		}
	})

	t.Run("with server restart", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		msgs := make([]Msg, 0)
		it, err := c.Messages()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		done := make(chan struct{})
		errs := make(chan error)
		publishTestMsgs(t, nc)
		go func() {
			for i := 0; i < 2*len(testMsgs); i++ {
				msg, err := it.Next()
				if err != nil {
					errs <- err
					return
				}
				msg.Ack()
				msgs = append(msgs, msg)
			}
			done <- struct{}{}
		}()
		time.Sleep(10 * time.Millisecond)
		// restart the server
		srv = restartBasicJSServer(t, srv)
		defer shutdownJSServerAndRemoveStorage(t, srv)
		time.Sleep(10 * time.Millisecond)
		publishTestMsgs(t, nc)

		select {
		case <-done:
			if len(msgs) != 2*len(testMsgs) {
				t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
			}
		case <-errs:
			t.Fatalf("Unexpected error: %s", err)
		}
	})
}

func TestPullConsumerConsume(t *testing.T) {
	testSubject := "FOO.123"
	testMsgs := []string{"m1", "m2", "m3", "m4", "m5"}
	publishTestMsgs := func(t *testing.T, nc *nats.Conn) {
		for _, msg := range testMsgs {
			if err := nc.Publish(testSubject, []byte(msg)); err != nil {
				t.Fatalf("Unexpected error during publish: %s", err)
			}
		}
	}

	t.Run("no options", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		msgs := make([]Msg, 0)
		wg := &sync.WaitGroup{}
		wg.Add(len(testMsgs))
		l, err := c.Consume(func(msg Msg, err error) {
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			msgs = append(msgs, msg)
			wg.Done()
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer l.Stop()

		publishTestMsgs(t, nc)
		wg.Wait()
		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("subscribe twice on the same consumer", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		l, err := c.Consume(func(msg Msg, err error) {})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer l.Stop()

		_, err = c.Consume(func(msg Msg, err error) {})
		if err == nil || !errors.Is(err, ErrConsumerHasActiveSubscription) {
			t.Fatalf("Expected error: %v; got: %v", ErrConsumerHasActiveSubscription, err)
		}
	})

	t.Run("subscribe, cancel subscription, then subscribe again", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		wg := sync.WaitGroup{}
		wg.Add(len(testMsgs))
		msgs := make([]Msg, 0)
		l, err := c.Consume(func(msg Msg, err error) {
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if err := msg.Ack(); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			msgs = append(msgs, msg)
			wg.Done()
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		publishTestMsgs(t, nc)
		wg.Wait()
		l.Stop()

		time.Sleep(10 * time.Millisecond)
		wg.Add(len(testMsgs))
		l, err = c.Consume(func(msg Msg, err error) {
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if err := msg.Ack(); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			msgs = append(msgs, msg)
			wg.Done()
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer l.Stop()
		publishTestMsgs(t, nc)
		wg.Wait()
		if len(msgs) != 2*len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		expectedMsgs := append(testMsgs, testMsgs...)
		for i, msg := range msgs {
			if string(msg.Data()) != expectedMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with custom batch size", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// subscribe to next request subject to verify how many next requests were sent
		sub, err := nc.SubscribeSync(fmt.Sprintf("$JS.API.CONSUMER.MSG.NEXT.foo.%s", c.CachedInfo().Name))
		if err != nil {
			t.Fatalf("Error on subscribe: %v", err)
		}

		msgs := make([]Msg, 0)
		wg := &sync.WaitGroup{}
		wg.Add(len(testMsgs))
		l, err := c.Consume(func(msg Msg, err error) {
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			msgs = append(msgs, msg)
			wg.Done()
		}, WithConsumeBatchSize(2))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer l.Stop()

		publishTestMsgs(t, nc)
		wg.Wait()
		requestsNum, _, err := sub.Pending()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// with batch size set to 2, and 5 messages published on subject, there should be a total of 3 requests sent
		if requestsNum != 3 {
			t.Fatalf("Unexpected number of requests sent; want 3; got %d", requestsNum)
		}

		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with custom max bytes", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// subscribe to next request subject to verify how many next requests were sent
		sub, err := nc.SubscribeSync(fmt.Sprintf("$JS.API.CONSUMER.MSG.NEXT.foo.%s", c.CachedInfo().Name))
		if err != nil {
			t.Fatalf("Error on subscribe: %v", err)
		}

		publishTestMsgs(t, nc)
		msgs := make([]Msg, 0)
		wg := &sync.WaitGroup{}
		wg.Add(len(testMsgs))
		l, err := c.Consume(func(msg Msg, err error) {
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			msgs = append(msgs, msg)
			wg.Done()
		}, WithConsumeMaxBytes(140))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer l.Stop()

		wg.Wait()
		requestsNum, _, err := sub.Pending()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// with batch size set to 2, and 5 messages published on subject, there should be a total of 3 requests sent
		if requestsNum != 3 {
			t.Fatalf("Unexpected number of requests sent; want 3; got %d", requestsNum)
		}

		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with invalid batch size", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = c.Consume(func(_ Msg, _ error) {
		}, WithConsumeBatchSize(-1))
		if err == nil || !errors.Is(err, ErrInvalidOption) {
			t.Fatalf("Expected error: %v; got: %v", ErrInvalidOption, err)
		}
	})

	t.Run("with custom expiry", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// subscribe to next request subject to verify how many next requests were sent
		sub, err := nc.SubscribeSync(fmt.Sprintf("$JS.API.CONSUMER.MSG.NEXT.foo.%s", c.CachedInfo().Name))
		if err != nil {
			t.Fatalf("Error on subscribe: %v", err)
		}

		msgs := make([]Msg, 0)
		wg := &sync.WaitGroup{}
		wg.Add(len(testMsgs))
		l, err := c.Consume(func(msg Msg, err error) {
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			msgs = append(msgs, msg)
			wg.Done()
		}, WithConsumeExpiry(50*time.Millisecond), WithConsumeHeartbeat(20*time.Millisecond))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer l.Stop()

		time.Sleep(60 * time.Millisecond)
		publishTestMsgs(t, nc)
		wg.Wait()

		requestsNum, _, err := sub.Pending()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// with expiry set to 50ms, and 60ms wait before messages are published, there should be a total of 2 requests sent to the server
		if requestsNum != 2 {
			t.Fatalf("Unexpected number of requests sent; want 3; got %d", requestsNum)
		}
		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with timeout on pull request, pending messages left", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// subscribe to next request subject to verify how many next requests were sent
		sub, err := nc.SubscribeSync(fmt.Sprintf("$JS.API.CONSUMER.MSG.NEXT.foo.%s", c.CachedInfo().Name))
		if err != nil {
			t.Fatalf("Error on subscribe: %v", err)
		}

		msgs := make([]Msg, 0)
		wg := &sync.WaitGroup{}
		wg.Add(4 * len(testMsgs))
		l, err := c.Consume(func(msg Msg, err error) {
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			msgs = append(msgs, msg)
			wg.Done()
		}, WithConsumeExpiry(50*time.Millisecond), WithConsumeHeartbeat(20*time.Millisecond))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer l.Stop()

		time.Sleep(60 * time.Millisecond)

		// publish messages in 4 batches, with pause between each batch
		// on the second pull request, we should get timeout error, wchich triggers another batch
		publishTestMsgs(t, nc)
		time.Sleep(40 * time.Millisecond)
		publishTestMsgs(t, nc)
		time.Sleep(40 * time.Millisecond)
		publishTestMsgs(t, nc)
		time.Sleep(40 * time.Millisecond)
		publishTestMsgs(t, nc)
		wg.Wait()

		requestsNum, _, err := sub.Pending()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// with expiry set to 50ms, and 60ms wait before messages are published, there should be a total of 2 requests sent to the server
		if requestsNum != 4 {
			t.Fatalf("Unexpected number of requests sent; want 3; got %d", requestsNum)
		}
		if len(msgs) != 4*len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
	})

	t.Run("with invalid expiry", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		_, err = c.Consume(func(_ Msg, _ error) {
		}, WithConsumeExpiry(-1))
		if err == nil || !errors.Is(err, ErrInvalidOption) {
			t.Fatalf("Expected error: %v; got: %v", ErrInvalidOption, err)
		}
	})

	t.Run("with idle heartbeat", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		msgs := make([]Msg, 0)
		wg := &sync.WaitGroup{}
		wg.Add(len(testMsgs))
		l, err := c.Consume(func(msg Msg, err error) {
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			msgs = append(msgs, msg)
			wg.Done()
		}, WithConsumeHeartbeat(10*time.Millisecond))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer l.Stop()

		publishTestMsgs(t, nc)
		wg.Wait()
		if len(msgs) != len(testMsgs) {
			t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
		}
		for i, msg := range msgs {
			if string(msg.Data()) != testMsgs[i] {
				t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
			}
		}
	})

	t.Run("with idle heartbeat, server shutdown", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		defer shutdownJSServerAndRemoveStorage(t, srv)
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		wg := &sync.WaitGroup{}
		wg.Add(1)
		l, err := c.Consume(func(_ Msg, err error) {
			if !errors.Is(err, ErrNoHeartbeat) {
				t.Fatalf("Unexpected error: %v; expected: %v", err, ErrNoHeartbeat)
			}
			wg.Done()
		}, WithConsumeHeartbeat(10*time.Millisecond))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer l.Stop()
		shutdownJSServerAndRemoveStorage(t, srv)
		wg.Wait()
	})

	t.Run("with server restart", func(t *testing.T) {
		srv := RunBasicJetStreamServer()
		nc, err := nats.Connect(srv.ClientURL())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		js, err := New(nc)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer nc.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := js.CreateStream(ctx, StreamConfig{Name: "foo", Subjects: []string{"FOO.*"}})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		wg := &sync.WaitGroup{}
		wg.Add(2 * len(testMsgs))
		msgs := make([]Msg, 0)
		publishTestMsgs(t, nc)
		l, err := c.Consume(func(msg Msg, err error) {
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			msgs = append(msgs, msg)
			wg.Done()
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer l.Stop()
		time.Sleep(10 * time.Millisecond)
		// restart the server
		srv = restartBasicJSServer(t, srv)
		defer shutdownJSServerAndRemoveStorage(t, srv)
		time.Sleep(10 * time.Millisecond)
		publishTestMsgs(t, nc)
		wg.Wait()
	})
}

func TestPullConsumerStream_WithCluster(t *testing.T) {
	testSubject := "FOO.123"
	testMsgs := []string{"m1", "m2", "m3", "m4", "m5"}
	publishTestMsgs := func(t *testing.T, nc *nats.Conn) {
		for _, msg := range testMsgs {
			if err := nc.Publish(testSubject, []byte(msg)); err != nil {
				t.Fatalf("Unexpected error during publish: %s", err)
			}
		}
	}

	name := "cluster"
	stream := StreamConfig{
		Name:     name,
		Replicas: 1,
		Subjects: []string{"FOO.*"},
	}

	t.Run("no options", func(t *testing.T) {
		withJSClusterAndStream(t, name, 3, stream, func(t *testing.T, subject string, srvs ...*jsServer) {
			nc, err := nats.Connect(srvs[0].ClientURL())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			js, err := New(nc)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer nc.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := js.Stream(ctx, stream.Name)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			msgs := make([]Msg, 0)
			wg := &sync.WaitGroup{}
			wg.Add(len(testMsgs))
			l, err := c.Consume(func(msg Msg, err error) {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				msgs = append(msgs, msg)
				wg.Done()
			})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer l.Stop()

			publishTestMsgs(t, nc)
			wg.Wait()
			if len(msgs) != len(testMsgs) {
				t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
			}
			for i, msg := range msgs {
				if string(msg.Data()) != testMsgs[i] {
					t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
				}
			}
		})
	})

	t.Run("subscribe, cancel subscription, then subscribe again", func(t *testing.T) {
		withJSClusterAndStream(t, name, 3, stream, func(t *testing.T, subject string, srvs ...*jsServer) {
			nc, err := nats.Connect(srvs[0].ClientURL())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			js, err := New(nc)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer nc.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			s, err := js.Stream(ctx, stream.Name)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			c, err := s.CreateConsumer(ctx, ConsumerConfig{AckPolicy: AckExplicitPolicy})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			wg := sync.WaitGroup{}
			wg.Add(len(testMsgs))
			msgs := make([]Msg, 0)
			l, err := c.Consume(func(msg Msg, err error) {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if err := msg.Ack(); err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				msgs = append(msgs, msg)
				if len(msgs) == 5 {
					cancel()
				}
				wg.Done()
			})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			publishTestMsgs(t, nc)
			wg.Wait()
			l.Stop()

			time.Sleep(10 * time.Millisecond)
			wg.Add(len(testMsgs))
			defer cancel()
			l, err = c.Consume(func(msg Msg, err error) {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if err := msg.Ack(); err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				msgs = append(msgs, msg)
				wg.Done()
			})
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer l.Stop()
			publishTestMsgs(t, nc)
			wg.Wait()
			if len(msgs) != 2*len(testMsgs) {
				t.Fatalf("Unexpected received message count; want %d; got %d", len(testMsgs), len(msgs))
			}
			expectedMsgs := append(testMsgs, testMsgs...)
			for i, msg := range msgs {
				if string(msg.Data()) != expectedMsgs[i] {
					t.Fatalf("Invalid msg on index %d; expected: %s; got: %s", i, testMsgs[i], string(msg.Data()))
				}
			}
		})
	})
}
