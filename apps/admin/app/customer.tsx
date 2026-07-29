import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, StatusPill, KeyValue, Button, ListRow, Timeline,
  Metric, Toast, Segmented, Avatar, BuildingIcon,
  color, font, inrShort, space,
} from '@dwellm8/mobile-shared';
import { customers } from '../src/data/mock';

/** The record an administrator reads while a customer is on the phone. */

export default function CustomerScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const c = customers.find((x) => x.id === id) ?? customers[0];
  const [tab, setTab] = useState('Overview');
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  return (
    <>
      <BackHeader title={c.name} subtitle={c.kind} onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
            {c.kind === 'Organisation'
              ? <View style={s.icon}><BuildingIcon size={24} /></View>
              : <Avatar initials={c.name.split(' ').map((w) => w[0]).join('')} size={46} />}
            <View style={{ flex: 1 }}>
              <Text style={s.title}>{c.name}</Text>
              <Text style={s.sub}>{c.detail}</Text>
            </View>
            <StatusPill text={c.state} tone={c.state === 'Active' ? 'green' : c.state === 'Suspended' ? 'red' : 'amber'} />
          </View>

          {c.gmvPaise ? (
            <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
              <Metric value={inrShort(c.gmvPaise)} label="lifetime volume" tone="blue" />
              <Metric value="2.99%" label="platform fee" tone="violet" />
              <Metric value="0" label="open disputes" tone="green" />
            </View>
          ) : null}
        </Card>

        <View style={{ marginBottom: space(3) }}>
          <Segmented items={['Overview', 'Events']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Overview' ? (
          <>
            <Card>
              <Text style={s.h}>Account</Text>
              <KeyValue k="Customer since" v={c.since} />
              <KeyValue k="Plan" v="Transaction fee, 2.99% at payout" />
              <KeyValue k="KYC" v={c.state === 'Suspended' ? 'Failed — re-verification required' : 'Verified'} tone={c.state === 'Suspended' ? 'red' : 'green'} />
              <KeyValue k="Payout account" v="HDFC ••8821 (finance only)" />
              <KeyValue k="Aadhaar" v="Never stored" last />
            </Card>

            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              <ListRow title="Open the console record" subtitle="Full history, exports and corrections" onPress={() => {}} />
              <ListRow title="Impersonate for support" subtitle="Read-only, time-boxed, fully audited" onPress={() => say('Read-only session started — 15 minutes, audited')} />
              <ListRow
                title={c.state === 'Suspended' ? 'Reinstate' : 'Suspend collections'}
                subtitle="Requires a reason and a second approver"
                onPress={() => say('Sent for second approval')}
                last
              />
            </Card>
          </>
        ) : (
          <Card>
            <Text style={s.h}>Recent events</Text>
            <Timeline
              items={[
                { at: 'Today, 09:12', what: 'Mandate presentment failed for 3 tenancies' },
                { at: 'Today, 07:55', what: 'Payout exception raised above the cap' },
                { at: '28 Jul', what: 'Duplicate payment reported by a tenant' },
                { at: '25 Jul', what: 'Payout released — ₹7,58,286' },
                { at: '01 Jul', what: 'Two new properties onboarded' },
              ]}
            />
          </Card>
        )}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  icon: { width: 46, height: 46, borderRadius: 23, backgroundColor: '#F3F7FB', alignItems: 'center', justifyContent: 'center' },
  title: { ...font.h3, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
});
