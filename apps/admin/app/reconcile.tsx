import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Metric, ListRow, KeyValue, Button, ProgressBar,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { reconciliation } from '../src/data/mock';

/**
 * Reconciliation status.
 *
 * Read-only on purpose: matching a credit to a tenancy is comparison work
 * across many records, and that belongs on the web console. The app exists to
 * tell an on-call administrator whether the pile is growing.
 */

export default function Reconcile() {
  const router = useRouter();
  const pct = Math.round((reconciliation.matched / reconciliation.bankCredits) * 100);

  return (
    <>
      <BackHeader title="Reconciliation" subtitle={reconciliation.date} onBack={() => router.back()} />
      <Screen>
        <Card>
          <View style={{ flexDirection: 'row', justifyContent: 'space-between' }}>
            <Text style={s.label}>Matched today</Text>
            <Text style={s.label}>{reconciliation.matched} of {reconciliation.bankCredits}</Text>
          </View>
          <ProgressBar pct={pct} tint={pct > 95 ? color.positive : '#D89A2B'} />
          <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
            <Metric value={String(reconciliation.unmatched)} label="unmatched credits" tone="red" />
            <Metric value={inr(reconciliation.unmatchedPaise, { noPaise: true })} label="unmatched value" tone="amber" />
            <Metric value={inr(reconciliation.suspensePaise, { noPaise: true })} label="in suspense" tone="violet" />
          </View>
        </Card>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {reconciliation.items.map((it, i) => (
            <ListRow
              key={it.id}
              title={it.ref}
              subtitle={it.why}
              right={<Text style={s.amt}>{inr(it.paise, { noPaise: true })}</Text>}
              last={i === reconciliation.items.length - 1}
            />
          ))}
        </Card>

        <Card>
          <Text style={s.h}>Why none of this auto-matches</Text>
          <KeyValue k="Guessing a tenancy" v="Never" tone="red" />
          <KeyValue k="Holding in suspense" v="Always, until matched" />
          <KeyValue k="Releasing a payout on it" v="Never" tone="red" last />
          <Text style={s.note}>
            An unmatched credit is somebody's rent. Matching happens on the console where the
            narration, the invoice and the tenancy can be seen side by side.
          </Text>
          <Button label="Open the console" tone="secondary" onPress={() => {}} style={{ marginTop: space(4) }} />
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  label: { ...font.small, color: color.inkSoft },
  amt: { ...font.title, color: color.inkStrong },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
