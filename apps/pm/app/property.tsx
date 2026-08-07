import React from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, ListRow, Metric,
  HouseArt, color, font, inr, space, type OpsUnit,
} from '@dwellm8/mobile-shared';
import { usePropertyRecord } from '../src/data/property';

/** One property and every unit in it — the record a manager opens on site. */
export default function PropertyScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const { loading, error, property, units, occupied, vacant, spokenFor, rentRollPaise } =
    usePropertyRecord(id);

  return (
    <>
      <BackHeader
        title={property?.name ?? 'Property'}
        subtitle={property ? [property.locality, property.city].filter(Boolean).join(', ') : undefined}
        onBack={() => router.back()}
      />
      <Screen>
        {loading ? (
          <View style={s.waiting}><ActivityIndicator /></View>
        ) : null}
        {error ? <Card><Text style={s.empty}>{error}</Text></Card> : null}

        {property ? (
          <>
            <Card padded={false} style={{ overflow: 'hidden' }}>
              <View style={s.art}><HouseArt size={150} /></View>
              <View style={{ padding: space(4) }}>
                <Text style={s.title}>{property.name}</Text>
                <Text style={s.sub}>{property.address_line1}</Text>
                <Text style={s.rent}>{inr(rentRollPaise, { noPaise: true })} per month let</Text>
                <KeyValue k="Reference" v={property.code} />
                <KeyValue k="Kind" v={property.kind} last />
              </View>
            </Card>

            <View style={s.metrics}>
              <Metric value={String(occupied)} label="let" tone="green" />
              <Metric value={String(vacant)} label="empty" tone="amber" />
              {spokenFor ? <Metric value={String(spokenFor)} label="taken" tone="amber" /> : null}
            </View>

            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              <Text style={[s.h, { marginTop: space(4) }]}>Units</Text>
              {units.map((u, i) => (
                <ListRow
                  key={u.id}
                  title={u.code}
                  subtitle={u.tenant ?? 'Vacant'}
                  meta={metaFor(u)}
                  right={<StatusPill text={pillFor(u)} tone={u.lease_id ? 'green' : 'amber'} />}
                  last={i === units.length - 1}
                />
              ))}
              {!units.length ? (
                <Text style={s.empty}>No units on this property yet.</Text>
              ) : null}
            </Card>
          </>
        ) : null}
      </Screen>
    </>
  );
}

// A unit whose tenancy starts later is empty today and still not on offer, so
// it reads as taken from a date rather than as free (#304).
function pillFor(u: OpsUnit): string {
  if (u.let_from) return 'From ' + u.let_from;
  return u.lease_id ? 'Let' : 'Empty';
}

function metaFor(u: OpsUnit): string {
  if (u.let_from) {
    return `${inr(u.rent_amount_minor ?? 0, { noPaise: true })} · from ${u.let_from}`;
  }
  if (u.lease_id) {
    return `${inr(u.rent_amount_minor ?? 0, { noPaise: true })} · to ${u.lease_ends ?? '—'}`;
  }
  return u.kind;
}

const s = StyleSheet.create({
  art: { backgroundColor: '#EAF1FB', alignItems: 'center', paddingVertical: space(4) },
  title: { ...font.h2, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
  rent: { ...font.h3, color: color.accent, marginTop: space(3), marginBottom: space(2) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  metrics: { flexDirection: 'row', gap: 10, marginBottom: space(3) },
  waiting: { paddingVertical: space(6), alignItems: 'center' },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(6) },
});
