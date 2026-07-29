import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, StatusPill, Button, ActionBar, KeyValue, Timeline,
  Toast, ListRow, ChoiceRow,
  color, font, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { alerts } from '../src/data/mock';

/**
 * One alert, and the small set of interventions an on-call admin may run from
 * a phone. Anything that changes configuration is not here by design.
 */

const sevTone: Record<string, Tone> = { P1: 'red', P2: 'amber', P3: 'blue' };

export default function AlertScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const a = alerts.find((x) => x.id === id) ?? alerts[0];

  const [toast, setToast] = useState<string | null>(null);
  const [action, setAction] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2800);
  };

  const interventions = [
    { k: 'pause', label: 'Pause automatic mandate retries', hint: 'Stops re-presentment platform-wide for 60 minutes' },
    { k: 'notify', label: 'Notify affected managers', hint: 'Sends the approved incident template to 38 organisations' },
    { k: 'scale', label: 'Scale the consumer group', hint: 'Adds capacity; replay-safe and reversible' },
    { k: 'escalate', label: 'Escalate to the payments on-call', hint: 'Pages the secondary rota' },
  ];

  return (
    <>
      <BackHeader title={a.severity + ' incident'} subtitle={a.service} onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <StatusPill text={a.severity} tone={sevTone[a.severity]} />
            <StatusPill text={a.state} tone={a.state === 'Firing' ? 'red' : a.state === 'Resolved' ? 'green' : 'blue'} dot />
            <View style={{ flex: 1 }} />
            <Text style={s.at}>{a.at}</Text>
          </View>
          <Text style={s.title}>{a.title}</Text>
          <Text style={s.body}>{a.detail}</Text>
        </Card>

        <Card>
          <Text style={s.h}>Runbook</Text>
          <Text style={s.body}>{a.runbook}</Text>
          <View style={{ marginTop: space(3) }}>
            <KeyValue k="Owner" v="Payments on-call" />
            <KeyValue k="Blast radius" v="38 organisations · 214 tenancies" />
            <KeyValue k="Money at risk" v="₹9,84,200 of scheduled debits" tone="red" last />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>Intervene</Text>
          {interventions.map((i, idx) => (
            <ChoiceRow
              key={i.k}
              label={i.label}
              hint={i.hint}
              selected={action === i.k}
              onPress={() => setAction(i.k)}
              last={idx === interventions.length - 1}
            />
          ))}
          <Text style={s.note}>
            Every intervention is written to the audit trail against your name. Nothing here changes
            fees, rule tables or a customer's configuration.
          </Text>
        </Card>

        <Card>
          <Text style={s.h}>Timeline</Text>
          <Timeline
            items={[
              { at: '08:32', what: 'Failure rate crossed the warning threshold' },
              { at: '09:12', what: 'Alert fired to the primary rota' },
              { at: '09:14', what: 'Auto-retry backoff widened' },
              { at: 'Now', what: 'Awaiting acknowledgement', done: false },
            ]}
          />
        </Card>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          <ListRow title="Open the console" subtitle="Full metrics, affected list and export" onPress={() => {}} last />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Acknowledge" tone="secondary" onPress={() => say('Acknowledged — the rota has been told')} style={{ flex: 1 }} />
        <Button
          label="Run action"
          onPress={() => say(action ? 'Intervention started and audited' : 'Choose an intervention first')}
          disabled={!action}
          style={{ flex: 1 }}
        />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  at: { ...font.small, color: color.inkFaint },
  title: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  h: { ...font.h3, color: color.inkStrong },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
