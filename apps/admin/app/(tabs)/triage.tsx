import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, Segmented, ListRow, StatusPill, Metric,
  Button, KeyValue,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { disputes, reconciliation } from '../../src/data/mock';

/** Dispute and reconciliation triage — decide, or route it to the right desk. */

const stateTone: Record<string, Tone> = {
  New: 'red', Investigating: 'amber', 'With provider': 'violet', Resolved: 'green',
};

export default function Triage() {
  const router = useRouter();
  const [tab, setTab] = useState('Disputes');

  return (
    <>
      <AppHeader title="Triage" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <View style={{ marginTop: space(3), marginBottom: space(2) }}>
          <Segmented items={['Disputes', 'Reconciliation']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Disputes' ? (
          <>
            <View style={s.metrics}>
              <Metric value={String(disputes.length)} label="open disputes" tone="amber" />
              <Metric value="1" label="over SLA" tone="red" />
              <Metric value={inr(disputes.reduce((a, d) => a + d.amountPaise, 0), { noPaise: true })} label="value in dispute" tone="blue" />
            </View>

            {disputes.map((d) => (
              <Card key={d.id}>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                  <StatusPill text={d.state} tone={stateTone[d.state]} dot />
                  <View style={{ flex: 1 }} />
                  <Text style={s.amt}>{inr(d.amountPaise, { noPaise: true })}</Text>
                </View>
                <Text style={s.title}>{d.title}</Text>
                <Text style={s.sub}>{d.org} · raised {d.raised} · {d.age} old</Text>
                <Text style={s.body}>{d.summary}</Text>
                <Button label="Open" tone="secondary" small onPress={() => router.push(`/dispute?id=${d.id}`)} style={{ marginTop: space(4) }} />
              </Card>
            ))}
          </>
        ) : (
          <>
            <View style={s.metrics}>
              <Metric value={`${reconciliation.matched}/${reconciliation.bankCredits}`} label="credits matched" tone="green" />
              <Metric value={String(reconciliation.unmatched)} label="unmatched" tone="red" />
              <Metric value={inr(reconciliation.suspensePaise, { noPaise: true })} label="in suspense" tone="amber" />
            </View>

            <Card>
              <Text style={s.h}>{reconciliation.date}</Text>
              <Text style={s.body}>
                Unmatched credits sit in suspense and are never guessed into a tenancy. Matching them
                is web console work; what the app does is tell you the pile is growing.
              </Text>
              <KeyValue k="Unmatched value" v={inr(reconciliation.unmatchedPaise)} tone="red" last />
            </Card>

            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              {reconciliation.items.map((it, i) => (
                <ListRow
                  key={it.id}
                  title={it.ref}
                  subtitle={it.why}
                  right={<Text style={s.amt}>{inr(it.paise, { noPaise: true })}</Text>}
                  onPress={() => router.push('/reconcile')}
                  last={i === reconciliation.items.length - 1}
                />
              ))}
            </Card>
          </>
        )}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginBottom: space(3) },
  title: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  amt: { ...font.title, color: color.inkStrong },
  h: { ...font.h3, color: color.inkStrong },
});
