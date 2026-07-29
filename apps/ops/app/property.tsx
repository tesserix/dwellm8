import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Button, ListRow, Segmented,
  Timeline, HouseArt, PhoneIcon, ChatIcon, DocIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { propertiesOps, tickets } from '../src/data/mock';

/** A unit and its tenancy — the record a manager opens on site. */
export default function PropertyScreen() {
  const router = useRouter();
  const { id, unit } = useLocalSearchParams<{ id?: string; unit?: string }>();
  const p = propertiesOps.find((x) => x.id === id) ?? propertiesOps[0];
  const u = p.units.find((x) => x.id === unit) ?? p.units[0];
  const [tab, setTab] = useState('Tenancy');

  const unitTickets = tickets.filter((t) => t.unit.includes(u.label) || t.unit.includes(p.name));

  return (
    <>
      <BackHeader title={u.label} subtitle={p.name} onBack={() => router.back()} />
      <Screen>
        <Card padded={false} style={{ overflow: 'hidden' }}>
          <View style={s.art}><HouseArt size={150} /></View>
          <View style={{ padding: space(4) }}>
            <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
              <Text style={s.title}>{u.label}</Text>
              <StatusPill text={u.status} tone={u.status === 'Occupied' ? 'green' : u.status === 'Vacant' ? 'amber' : 'violet'} />
            </View>
            <Text style={s.sub}>{p.locality}</Text>
            <Text style={s.rent}>{inr(u.rentPaise, { noPaise: true })} per month</Text>
          </View>
        </Card>

        <View style={{ marginBottom: space(3) }}>
          <Segmented items={['Tenancy', 'Owner', 'Jobs']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Tenancy' ? (
          <>
            <Card>
              <Text style={s.h}>Tenancy</Text>
              <KeyValue k="Tenant" v={u.tenant ?? 'Vacant'} />
              <KeyValue k="Rent paid to" v={u.paidTo ?? '—'} tone={u.paidTo ? 'green' : undefined} />
              <KeyValue k="Lease ends" v={u.leaseEnds ?? '—'} />
              <KeyValue k="Deposit held" v={inr(u.rentPaise * 3)} />
              <KeyValue k="Notice period" v="60 days" last />
              {u.tenant ? (
                <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
                  <Button label="Call" tone="secondary" small icon={<PhoneIcon size={17} c={color.accent} />} onPress={() => {}} style={{ flex: 1 }} />
                  <Button label="Message" tone="secondary" small icon={<ChatIcon size={17} c={color.accent} />} onPress={() => router.push('/thread?id=th1')} style={{ flex: 1 }} />
                </View>
              ) : (
                <Button label="Create a listing" onPress={() => router.push('/leads')} style={{ marginTop: space(4) }} />
              )}
            </Card>

            <Card>
              <Text style={s.h}>Documents</Text>
              {['Leave and licence agreement — executed.pdf', 'Move-in condition report.pdf', 'Police verification acknowledgement.pdf'].map((d, i) => (
                <ListRow key={d} left={<DocIcon size={20} c={color.inkFaint} />} title={d} onPress={() => {}} last={i === 2} />
              ))}
            </Card>

            <Card>
              <Text style={s.h}>Tenancy history</Text>
              <Timeline
                items={[
                  { at: '15 Apr 2026', what: 'Agreement executed and eSigned' },
                  { at: '15 Apr 2026', what: 'Move-in inspection completed' },
                  { at: '20 Apr 2026', what: 'Deposit received and acknowledged' },
                  { at: '05 Jul 2026', what: 'Rent invoice raised' },
                  { at: '15 Apr 2027', what: 'Renewal decision due', done: false },
                ]}
              />
            </Card>
          </>
        ) : null}

        {tab === 'Owner' ? (
          <Card>
            <Text style={s.h}>Owner</Text>
            <KeyValue k="Name" v={p.owner} />
            <KeyValue k="Phone" v={p.ownerPhone} />
            <KeyValue k="Management fee" v="8% of rent collected" />
            <KeyValue k="Payout cycle" v="Fortnightly, to HDFC ••4471" />
            <KeyValue k="Spend authority" v="₹10,000 per job without approval" last />
            <Button label="Message the owner" tone="secondary" onPress={() => router.push('/thread?id=th2')} style={{ marginTop: space(4) }} />
          </Card>
        ) : null}

        {tab === 'Jobs' ? (
          <Card padded={false} style={{ paddingHorizontal: space(4) }}>
            {unitTickets.length ? (
              unitTickets.map((t, i) => (
                <ListRow
                  key={t.id}
                  title={t.title}
                  subtitle={`${t.status} · ${t.category}`}
                  meta={`Raised ${t.raised}${t.vendor ? ` · ${t.vendor}` : ''}`}
                  onPress={() => router.push(`/ticket?id=${t.id}`)}
                  last={i === unitTickets.length - 1}
                />
              ))
            ) : (
              <Text style={s.empty}>No jobs against this unit.</Text>
            )}
          </Card>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  art: { backgroundColor: '#EAF1FB', alignItems: 'center', paddingVertical: space(4) },
  title: { ...font.h2, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
  rent: { ...font.h3, color: color.accent, marginTop: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(6) },
});
