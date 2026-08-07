import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator, Alert } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  ActionBar, BackHeader, Button, Card, ChoiceRow, Field, KeyValue, Screen,
  SectionTitle, StatusPill, Banner, describeHistory, monthLabel, packReadiness,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import type { ApplicantAddress, ApplicantPerson } from '@dwellm8/mobile-shared';
import {
  saveAddresses, saveHousehold, savePack, submitPack,
  useAddressHistory, useApplicantPack,
} from '../src/data/screening';

/**
 * The applicant pack the manager collects (#258, #259): who is applying, who
 * else moves in, and five years of addresses with the holes named.
 */

const emptyPerson: ApplicantPerson = { role: 'co_applicant', full_name: '' };
const emptyAddress: ApplicantAddress = { kind: 'rented', line1: '', city: '', from: '' };

export default function Pack() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { id } = useLocalSearchParams<{ id: string }>();
  const pack = useApplicantPack(id);
  const history = useAddressHistory(id);

  const [name, setName] = useState('');
  const [occupants, setOccupants] = useState('1');
  const [nonResident, setNonResident] = useState(false);
  const [people, setPeople] = useState<ApplicantPerson[]>([]);
  const [addresses, setAddresses] = useState<ApplicantAddress[]>([]);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const p = pack.data;
    if (!p) return;
    setName(p.full_name ?? '');
    setOccupants(String(p.occupants ?? 1));
    setNonResident(p.tax_residency === 'non_resident');
    setPeople(p.people ?? []);
  }, [pack.data]);

  useEffect(() => {
    if (history.data) setAddresses(history.data.addresses);
  }, [history.data]);

  const run = async (what: () => Promise<void>) => {
    setBusy(true);
    try {
      await what();
    } catch (err) {
      Alert.alert('Not saved', (err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const saveProfile = () => run(() => savePack(id, {
    full_name: name.trim(),
    occupants: Number(occupants) || 1,
    tax_residency: nonResident ? 'non_resident' : 'resident',
  }));

  const submit = () => run(async () => {
    await submitPack(id);
    router.back();
  });

  const missing = pack.data ? packReadiness(pack.data) : ['Applicant name'];
  const submitted = pack.data?.state === 'submitted';

  if (pack.loading) {
    return (
      <>
        <BackHeader title="Applicant pack" onBack={goBack} />
        <View style={s.wait}><ActivityIndicator /></View>
      </>
    );
  }

  return (
    <>
      <BackHeader
        title="Applicant pack"
        subtitle={pack.data?.full_name || 'Not started'}
        onBack={goBack}
      />
      <Screen>
        {pack.error ? <Banner>{pack.error}</Banner> : null}
        {submitted ? <Banner>Submitted — the pack is now read-only.</Banner> : null}

        <SectionTitle>Applicant</SectionTitle>
        <Card>
          <Field label="Full name" value={name} onChange={setName} placeholder="As on the ID proof" autoCapitalize="words" />
          <Field label="People moving in" value={occupants} onChange={setOccupants} keyboardType="numeric" />
          <ChoiceRow
            label="Resident for tax"
            hint="Ordinary resident of India"
            selected={!nonResident}
            onPress={() => setNonResident(false)}
          />
          <ChoiceRow
            label="Non-resident (NRI/NRO)"
            hint="Rent is credited to an NRO account; TDS differs"
            selected={nonResident}
            onPress={() => setNonResident(true)}
            last
          />
          {!submitted ? (
            <Button label="Save applicant" onPress={saveProfile} disabled={busy || !name.trim()} style={{ marginTop: space(4) }} />
          ) : null}
        </Card>

        <SectionTitle>Household</SectionTitle>
        <Card>
          {people.map((p, i) => (
            <View key={p.id ?? `new-${i}`} style={i ? s.stacked : undefined}>
              <Field
                label={`Person ${i + 1} — name`}
                value={p.full_name}
                onChange={(v) => setPeople(people.map((x, j) => (j === i ? { ...x, full_name: v } : x)))}
                autoCapitalize="words"
              />
              <Field
                label="Relationship"
                value={p.relationship ?? ''}
                onChange={(v) => setPeople(people.map((x, j) => (j === i ? { ...x, relationship: v } : x)))}
                placeholder="spouse, parent, colleague"
              />
              <Field
                label="Phone"
                value={p.phone ?? ''}
                onChange={(v) => setPeople(people.map((x, j) => (j === i ? { ...x, phone: v } : x)))}
                placeholder="+9198…"
                keyboardType="phone-pad"
              />
              {!submitted ? (
                <Button label="Remove" tone="ghost" small onPress={() => setPeople(people.filter((_, j) => j !== i))} />
              ) : null}
            </View>
          ))}
          {!people.length ? <Text style={s.empty}>Nobody else recorded yet.</Text> : null}
          {!submitted ? (
            <View style={s.row}>
              <Button label="Add person" tone="secondary" small onPress={() => setPeople([...people, { ...emptyPerson }])} />
              <Button label="Save household" small disabled={busy} onPress={() => run(() => saveHousehold(id, people))} />
            </View>
          ) : null}
        </Card>

        <SectionTitle>Last five years</SectionTitle>
        <Card>
          {history.loading ? <ActivityIndicator /> : null}
          {history.data ? (
            <View style={{ marginBottom: space(3) }}>
              <StatusPill
                text={describeHistory(history.data)}
                tone={history.data.complete ? 'green' : 'amber'}
              />
            </View>
          ) : null}
          {addresses.map((a, i) => (
            <View key={a.id ?? `addr-${i}`} style={i ? s.stacked : undefined}>
              <KeyValue k="Lived" v={`${monthLabel(a.from)} → ${monthLabel(a.to ?? '')}`} />
              <Field
                label="Address"
                value={a.line1}
                onChange={(v) => setAddresses(addresses.map((x, j) => (j === i ? { ...x, line1: v } : x)))}
              />
              <Field
                label="City"
                value={a.city}
                onChange={(v) => setAddresses(addresses.map((x, j) => (j === i ? { ...x, city: v } : x)))}
                autoCapitalize="words"
              />
              <Field
                label="From (YYYY-MM)"
                value={a.from}
                onChange={(v) => setAddresses(addresses.map((x, j) => (j === i ? { ...x, from: v } : x)))}
                placeholder="2023-07"
              />
              <Field
                label="To (YYYY-MM, blank if current)"
                value={a.to ?? ''}
                onChange={(v) => setAddresses(addresses.map((x, j) => (j === i ? { ...x, to: v || undefined } : x)))}
                placeholder="2025-02"
              />
              <Field
                label="Landlord name"
                value={a.landlord_name ?? ''}
                onChange={(v) => setAddresses(addresses.map((x, j) => (j === i ? { ...x, landlord_name: v } : x)))}
                autoCapitalize="words"
              />
              <Field
                label="Landlord phone"
                value={a.landlord_phone ?? ''}
                onChange={(v) => setAddresses(addresses.map((x, j) => (j === i ? { ...x, landlord_phone: v } : x)))}
                placeholder="+9198…"
                keyboardType="phone-pad"
              />
              {!submitted ? (
                <Button label="Remove" tone="ghost" small onPress={() => setAddresses(addresses.filter((_, j) => j !== i))} />
              ) : null}
            </View>
          ))}
          {!addresses.length && !history.loading ? (
            <Text style={s.empty}>No addresses yet — five years are needed for the reference checks.</Text>
          ) : null}
          {!submitted ? (
            <View style={s.row}>
              <Button label="Add address" tone="secondary" small onPress={() => setAddresses([...addresses, { ...emptyAddress }])} />
              <Button label="Save history" small disabled={busy} onPress={() => run(() => saveAddresses(id, addresses))} />
            </View>
          ) : null}
        </Card>

        {missing.length && !submitted ? (
          <Text style={s.missing}>Still needed: {missing.join(', ')}</Text>
        ) : null}
      </Screen>

      {!submitted ? (
        <ActionBar>
          <Button label="Submit pack" onPress={submit} disabled={busy || !!missing.length} style={{ flex: 1 }} />
        </ActionBar>
      ) : null}
    </>
  );
}

const s = StyleSheet.create({
  wait: { paddingVertical: space(8), alignItems: 'center' },
  row: { flexDirection: 'row', gap: 10, marginTop: space(2) },
  stacked: { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: color.line, paddingTop: space(4), marginTop: space(2) },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(4), textAlign: 'center' },
  missing: { ...font.small, color: color.inkSoft, marginHorizontal: space(4), marginTop: space(3) },
});
