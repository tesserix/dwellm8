import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, Segmented, ListRow, StatusPill, Metric,
  Button, KeyValue, Field,
  apiFromEnv, type FlaggedListing,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { disputes, reconciliation } from '../../src/data/mock';
import { DemoNotice } from '../../src/components/DemoNotice';

/** Dispute, moderation and reconciliation triage — decide, or route it on. */

const stateTone: Record<string, Tone> = {
  New: 'red', Investigating: 'amber', 'With provider': 'violet', Resolved: 'green',
};

/** The listing moderation queue (#143): reported or suspended, oldest first. */
function Moderation() {
  const client = useMemo(() => apiFromEnv(), []);
  const [queue, setQueue] = useState<FlaggedListing[]>([]);
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const [error, setError] = useState('');

  const load = useCallback(() => {
    if (!client) return;
    client.moderationListings().then(setQueue).catch((e: Error) => setError(e.message));
  }, [client]);
  useEffect(load, [load]);

  const act = (fn: () => Promise<void>) => {
    setError('');
    fn().then(load).catch((e: Error) => setError(e.message));
  };

  if (!client) {
    return (
      <Card>
        <Text style={s.body}>
          Demo mode — reported listings queue here once the app points at a live API, with
          suspend, reinstate and dismiss a tap away and every judgement audited.
        </Text>
      </Card>
    );
  }
  return (
    <>
      <View style={s.metrics}>
        <Metric value={String(queue.length)} label="waiting for review" tone={queue.length ? 'amber' : 'green'} />
        <Metric value={String(queue.filter((q) => q.state === 'suspended').length)} label="suspended" tone="red" />
      </View>
      {queue.map((q) => (
        <Card key={q.id}>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <StatusPill
              text={q.state === 'suspended' ? 'Suspended' : `${q.report_count} report${q.report_count === 1 ? '' : 's'}`}
              tone={q.state === 'suspended' ? 'red' : 'amber'}
              dot
            />
          </View>
          <Text style={s.title}>{q.headline}</Text>
          {q.suspended_reason ? <Text style={s.sub}>{q.suspended_reason}</Text> : null}
          {q.state === 'suspended' ? (
            <Button label="Reinstate" small onPress={() => act(() => client.reinstateListing(q.id))} style={{ marginTop: space(4) }} />
          ) : (
            <>
              <Field
                label="Suspension reason"
                value={reasons[q.id] ?? ''}
                onChange={(v: string) => setReasons((r) => ({ ...r, [q.id]: v }))}
                placeholder="Fraudulent advertisement"
              />
              <View style={{ flexDirection: 'row', gap: 10, marginTop: space(3) }}>
                <Button label="Dismiss report" tone="secondary" small onPress={() => act(() => client.dismissListingReports(q.id))} style={{ flex: 1 }} />
                <Button
                  label="Suspend"
                  small
                  onPress={() => act(() => client.suspendListing(q.id, (reasons[q.id] ?? '').trim()))}
                  style={{ flex: 1 }}
                />
              </View>
            </>
          )}
        </Card>
      ))}
      {!queue.length ? (
        <Card><Text style={s.body}>The queue is empty — nothing is waiting on a judgement.</Text></Card>
      ) : null}
      {error ? <Card><Text style={[s.sub, { color: '#B4232A' }]}>{error}</Text></Card> : null}
    </>
  );
}

export default function Triage() {
  const router = useRouter();
  const [tab, setTab] = useState('Disputes');

  return (
    <>
      <AppHeader title="Triage" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <DemoNotice />
        <View style={{ marginTop: space(3), marginBottom: space(2) }}>
          <Segmented items={['Disputes', 'Moderation', 'Reconciliation']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Moderation' ? <Moderation /> : null}
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
        ) : null}
        {tab === 'Reconciliation' ? (
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
        ) : null}
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
