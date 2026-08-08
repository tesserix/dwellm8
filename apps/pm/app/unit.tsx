import React from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, ListRow, Metric, SectionTitle, StatusPill,
  color, font, inr, space, useBack,
} from '@dwellm8/mobile-shared';
import { useUnitRecord } from '../src/data/unit';

/**
 * One flat (#338). The property record lists units; this is what opening one
 * gives you — the size, the meter numbers, the tenancy, and what it is
 * advertised at.
 */
export default function UnitScreen() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { id } = useLocalSearchParams<{ id?: string }>();
  const { loading, error, unit, property, tenancy, listing, ancillaries, bedrooms } =
    useUnitRecord(id);

  return (
    <>
      <BackHeader
        title={unit ? `Flat ${unit.code}` : 'Unit'}
        subtitle={property ? [property.name, property.locality].filter(Boolean).join(', ') : undefined}
        onBack={goBack}
      />
      <Screen>
        {loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {error ? <Card><Text style={s.empty}>{error}</Text></Card> : null}

        {unit ? (
          <>
            <View style={s.metrics}>
              <Metric
                value={bedrooms != null ? `${bedrooms} BHK` : '—'}
                label={bedrooms != null ? 'as advertised' : 'not advertised yet'}
                tone="violet"
              />
              <Metric
                value={unit.carpet_area_sqft ? String(Math.round(unit.carpet_area_sqft)) : '—'}
                label="carpet sq ft"
                tone="blue"
              />
              <Metric
                value={tenancy ? inr(tenancy.rent_amount_minor, { noPaise: true }) : 'Empty'}
                label={tenancy ? 'rent in force' : 'no tenancy'}
                tone={tenancy ? 'green' : 'amber'}
              />
            </View>

            {/* What a renter asks about before the rent (#354). */}
            <SectionTitle>What this flat is like</SectionTitle>
            <Card>
              <Text style={s.about}>{unit.about || 'Nothing written yet'}</Text>
              {unit.bathrooms != null ? <KeyValue k="Bathrooms" v={String(unit.bathrooms)} /> : null}
              {unit.balconies != null ? <KeyValue k="Balconies" v={String(unit.balconies)} /> : null}
              {unit.covered_parking != null ? (
                <KeyValue k="Covered parking" v={String(unit.covered_parking)} />
              ) : null}
              {unit.facing ? <KeyValue k="Facing" v={words(unit.facing)} /> : null}
              {unit.furnishing ? <KeyValue k="Furnishing" v={words(unit.furnishing)} /> : null}
              {unit.features?.length ? (
                <KeyValue k="In the flat" v={unit.features.map(words).join(', ')} last />
              ) : null}
            </Card>
            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              <ListRow
                title="Describe this flat"
                subtitle="Bathrooms, facing, furnishing and what is in it"
                right={<StatusPill
                  text={unit.about ? 'Written' : 'To write'}
                  tone={unit.about ? 'green' : 'neutral'}
                />}
                onPress={() => router.push(`/describe?unit=${unit.id}`)}
                last
              />
            </Card>

            <SectionTitle>The flat</SectionTitle>
            <Card>
              <KeyValue k="Reference" v={unit.code} />
              <KeyValue k="Kind" v={unit.kind} />
              <KeyValue k="Floor" v={String(unit.floor)} />
              <KeyValue k="Occupancy" v={unit.occupancy.replace(/_/g, ' ')} />
              {unit.builtup_area_sqft ? (
                <KeyValue k="Built-up area" v={`${Math.round(unit.builtup_area_sqft)} sq ft`} />
              ) : null}
              {unit.share_certificate_no ? (
                <KeyValue k="Share certificate" v={unit.share_certificate_no} />
              ) : null}
              {unit.electricity_consumer_no ? (
                <KeyValue k="Electricity consumer" v={unit.electricity_consumer_no} />
              ) : null}
              <KeyValue
                k="Water connection"
                v={unit.water_connection_no || 'Not recorded'}
                last
              />
            </Card>

            {tenancy ? (
              <>
                <SectionTitle>Who is in it</SectionTitle>
                <Card>
                  <View style={s.rowBetween}>
                    <Text style={s.name}>{tenancy.tenant || 'Tenant not named'}</Text>
                    <StatusPill
                      text={tenancy.let_from ? `From ${tenancy.let_from}` : tenancy.state.replace(/_/g, ' ')}
                      tone={tenancy.let_from ? 'amber' : 'green'}
                    />
                  </View>
                  <KeyValue k="Rent" v={`${inr(tenancy.rent_amount_minor, { noPaise: true })} a month`} />
                  {tenancy.due_day ? <KeyValue k="Due on" v={`Day ${tenancy.due_day}`} /> : null}
                  <KeyValue
                    k="Outstanding"
                    v={inr(tenancy.due_amount_minor, { noPaise: true })}
                    tone={tenancy.due_amount_minor > 0 ? 'red' : 'green'}
                  />
                  {tenancy.starts ? <KeyValue k="Term from" v={tenancy.starts} /> : null}
                  <KeyValue k="Term to" v={tenancy.ends || 'Open-ended'} last />
                </Card>
                <Card padded={false} style={{ paddingHorizontal: space(4) }}>
                  <ListRow
                    title="Open the tenancy"
                    subtitle="Position, receipts and what is owed"
                    onPress={() => router.push(`/arrear?id=${tenancy.lease_id}`)}
                    last
                  />
                </Card>
              </>
            ) : (
              <>
                <SectionTitle>Who is in it</SectionTitle>
                <Card><Text style={s.empty}>Nobody. This flat is free to let.</Text></Card>
              </>
            )}

            {listing ? (
              <>
                <SectionTitle>How it is advertised</SectionTitle>
                <Card>
                  {listing.headline ? <Text style={s.name}>{listing.headline}</Text> : null}
                  <KeyValue k="State" v={listing.state} />
                  <KeyValue k="Asking rent" v={inr(listing.rent_amount_minor, { noPaise: true })} />
                  {listing.deposit_amount_minor ? (
                    <KeyValue k="Deposit" v={inr(listing.deposit_amount_minor, { noPaise: true })} />
                  ) : null}
                  <KeyValue k="Available from" v={listing.available_from || 'Not stated'} last />
                </Card>
              </>
            ) : null}

            {ancillaries.length ? (
              <>
                <SectionTitle>Allotted to this flat</SectionTitle>
                <Card padded={false} style={{ paddingHorizontal: space(4) }}>
                  {ancillaries.map((a, i) => (
                    <ListRow
                      key={a.id}
                      title={a.code}
                      subtitle={a.kind}
                      last={i === ancillaries.length - 1}
                    />
                  ))}
                </Card>
              </>
            ) : null}
          </>
        ) : null}
      </Screen>
    </>
  );
}

// The record stores a vocabulary; a renter reads words.
const words = (v: string) => v.replace(/_/g, ' ').replace(/^./, (c) => c.toUpperCase());

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(3) },
  rowBetween: {
    flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between',
    marginBottom: space(2),
  },
  name: { ...font.h3, color: color.inkStrong },
  about: { ...font.body, color: color.ink, marginBottom: space(3) },
  waiting: { paddingVertical: space(6), alignItems: 'center' },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(5) },
});
