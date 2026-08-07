import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, StatusPill, Button, ActionBar, KeyValue, Timeline,
  Toast, ChoiceRow, Field,
  color, font, inr, space, useBack,
} from '@dwellm8/mobile-shared';
import { disputes } from '../src/data/mock';

/** A dispute, the ledger evidence, and the routes out of it. */

export default function DisputeScreen() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { id } = useLocalSearchParams<{ id?: string }>();
  const d = disputes.find((x) => x.id === id) ?? disputes[0];

  const [route, setRoute] = useState<string | null>(null);
  const [note, setNote] = useState('');
  const [toast, setToast] = useState<string | null>(null);

  const routes = [
    { k: 'refund', label: 'Refund to source', hint: 'Reverses the unallocated payment; ledger gets a reversing entry' },
    { k: 'provider', label: 'Raise with the provider', hint: 'Opens a case and pauses the SLA clock' },
    { k: 'manager', label: 'Return to the manager', hint: 'This is a management decision, not a platform one' },
    { k: 'close', label: 'Close — no platform fault', hint: 'Records the finding and tells both sides' },
  ];

  return (
    <>
      <BackHeader title={d.id.toUpperCase()} subtitle={d.org} onBack={goBack} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <StatusPill text={d.state} tone={d.state === 'New' ? 'red' : d.state === 'Resolved' ? 'green' : 'amber'} dot />
            <View style={{ flex: 1 }} />
            <Text style={s.amt}>{inr(d.amountPaise)}</Text>
          </View>
          <Text style={s.title}>{d.title}</Text>
          <Text style={s.body}>{d.summary}</Text>
          <View style={{ marginTop: space(3) }}>
            <KeyValue k="Raised" v={d.raised} />
            <KeyValue k="Age" v={d.age} tone={d.age.startsWith('5') ? 'red' : undefined} last />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>What the ledger says</Text>
          <KeyValue k="Debit 1 — UPI 09:14" v={inr(d.amountPaise)} tone="green" />
          <KeyValue k="Debit 2 — UPI 09:18" v={inr(d.amountPaise)} tone="green" />
          <KeyValue k="Postings against the invoice" v="1" />
          <KeyValue k="Unallocated" v={inr(d.amountPaise)} tone="amber" last />
          <Text style={s.note}>
            Postings are immutable. A correction is a reversing entry with a reason code, never an
            edit — which is why the second debit shows as unallocated rather than missing.
          </Text>
        </Card>

        <Card>
          <Text style={s.h}>Route it</Text>
          {routes.map((r, i) => (
            <ChoiceRow key={r.k} label={r.label} hint={r.hint} selected={route === r.k} onPress={() => setRoute(r.k)} last={i === routes.length - 1} />
          ))}
          <Field label="Note (recorded)" value={note} onChange={setNote} placeholder="Confirmed with the PSP that both debits settled." multiline />
        </Card>

        <Card>
          <Text style={s.h}>History</Text>
          <Timeline
            items={[
              { at: d.raised, what: 'Tenant reported through the manager' },
              { at: d.raised, what: 'Support triaged and attached the ledger extract' },
              { at: 'Yesterday', what: 'Provider confirmed both debits settled' },
              { at: 'Now', what: 'Awaiting your decision', done: false },
            ]}
          />
        </Card>
      </Screen>

      <ActionBar>
        <Button
          label="Apply"
          onPress={() => { setToast('Routed and audited'); setTimeout(() => setToast(null), 2600); }}
          disabled={!route}
          style={{ flex: 1 }}
        />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  title: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  amt: { ...font.h3, color: color.inkStrong },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
