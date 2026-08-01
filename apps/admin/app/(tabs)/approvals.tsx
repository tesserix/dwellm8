import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, ListRow, StatusPill, Metric, Toast,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { approvals, webOnly } from '../../src/data/mock';
import { DemoNotice } from '../../src/components/DemoNotice';

/**
 * The approval queue.
 *
 * Approvals are triageable on a phone; configuration is not. Anything that
 * needs comparison across many records stays on the web console, and this
 * screen says so rather than offering a cramped version of it.
 */

const riskTone: Record<string, Tone> = { Low: 'green', Medium: 'amber', High: 'red' };

export default function Approvals() {
  const router = useRouter();
  const [toast, setToast] = useState<string | null>(null);

  return (
    <>
      <AppHeader title="Approvals" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <DemoNotice />
        {toast ? <Toast text={toast} /> : null}

        <View style={s.metrics}>
          <Metric value={String(approvals.length)} label="waiting" tone="blue" />
          <Metric value="1" label="high risk" tone="red" />
          <Metric value="4 h" label="median age" tone="violet" />
        </View>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {approvals.map((a, i) => (
            <ListRow
              key={a.id}
              title={a.subject}
              subtitle={a.kind}
              meta={`${a.requestedBy} · ${a.at}${a.amountPaise ? ` · ${inr(a.amountPaise, { noPaise: true })}` : ''}`}
              right={<StatusPill text={a.risk} tone={riskTone[a.risk]} />}
              onPress={() => router.push(`/approval?id=${a.id}`)}
              last={i === approvals.length - 1}
              tone={a.risk === 'High' ? 'red' : undefined}
            />
          ))}
        </Card>

        <Card>
          <Text style={s.h}>Not on the phone</Text>
          <Text style={s.body}>
            These are deliberately absent from the app. They need review, comparison and a considered
            commit, and they are done on the Admin web console with the same permissions.
          </Text>
          {webOnly.map((w) => (
            <Text key={w} style={s.bullet}>· {w}</Text>
          ))}
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  bullet: { ...font.body, color: color.ink, marginTop: 8 },
});
