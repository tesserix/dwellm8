import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  ActionBar, BackHeader, Button, Card, ChoiceRow, Field, HouseArt, KeyValue,
  Screen, StatusPill, SwitchRow, Toast,
  apiFromEnv, color, font, inr, radius, space,
} from '@dwellm8/mobile-shared';
import type { OpsPortfolio, OwnerOnboarded } from '@dwellm8/mobile-shared';

/**
 * Onboard an owner (#240) — one guided flow, five small questions.
 *
 * Everything created here is real the moment it lands: the owner's identity
 * waits on the number that will claim it, the property sits in their books
 * under this firm's mandate, and a tenant named here sees the tenancy in
 * Live. No set-up on anybody else's side — that is the trick of it.
 */

// The property vocabulary the domain accepts — anything else is refused at
// registration, so the wizard offers these and nothing else.
const kinds = [
  { code: 'building', label: 'Apartment building' },
  { code: 'standalone', label: 'Independent house' },
  { code: 'society', label: 'Society or gated community' },
  { code: 'commercial', label: 'Commercial' },
  { code: 'coliving', label: 'Co-living or hostel' },
  { code: 'plot', label: 'Plot' },
];

const STEPS = ['The owner', 'The place', 'The units', 'Moving in', 'The once-over'] as const;

const todayIso = () => new Date().toISOString().slice(0, 10);
const plusMonthsIso = (months: number) => {
  const d = new Date();
  d.setMonth(d.getMonth() + months);
  return d.toISOString().slice(0, 10);
};

export default function Onboard() {
  const router = useRouter();
  const [step, setStep] = useState(0);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [done, setDone] = useState<OwnerOnboarded | null>(null);

  // The owner — one the firm already acts for, or somebody new
  const [portfolios, setPortfolios] = useState<OpsPortfolio[]>([]);
  const [existing, setExisting] = useState<OpsPortfolio | null>(null);
  const [ownerName, setOwnerName] = useState('');
  const [phone, setPhone] = useState('');
  const [email, setEmail] = useState('');
  // The place
  const [propName, setPropName] = useState('');
  const [propCode, setPropCode] = useState('');
  const [kind, setKind] = useState('building');
  const [addressLine1, setAddressLine1] = useState('');
  const [locality, setLocality] = useState('');
  const [city, setCity] = useState('');
  const [stateCode, setStateCode] = useState('');
  const [pin, setPin] = useState('');
  // The units
  const [units, setUnits] = useState('');
  const [carpetArea, setCarpetArea] = useState('');
  // Moving in (optional)
  const [withTenant, setWithTenant] = useState(false);
  const [tenantName, setTenantName] = useState('');
  const [tenantPhone, setTenantPhone] = useState('');
  const [tenantUnit, setTenantUnit] = useState('');
  const [rent, setRent] = useState('');
  const [deposit, setDeposit] = useState('');
  const [dueDay, setDueDay] = useState('5');
  const [startOn, setStartOn] = useState(todayIso());
  const [endOn, setEndOn] = useState(plusMonthsIso(11));
  const [ownerIndividual, setOwnerIndividual] = useState(true);
  const [ownerResident, setOwnerResident] = useState(true);

  const unitList = units.split(',').map((c) => c.trim()).filter(Boolean);

  useEffect(() => {
    const api = apiFromEnv();
    if (!api) return;
    api.opsPortfolios().then(setPortfolios).catch(() => setPortfolios([]));
  }, []);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 3200);
  };

  const ownerLabel = existing ? existing.owner_name : ownerName;

  const stepReady = [
    existing !== null || (ownerName.trim().length > 1 && phone.trim().length >= 10),
    propName.trim().length > 1 && addressLine1.trim().length > 1
      && locality.trim().length > 1 && city.trim().length > 1
      && /^[A-Z]{2}$/.test(stateCode.trim().toUpperCase()) && /^[1-9][0-9]{5}$/.test(pin.trim()),
    unitList.length > 0 && Number(carpetArea) > 0,
    !withTenant || (tenantName.trim().length > 1 && tenantPhone.trim().length >= 10
      && unitList.includes(tenantUnit.trim()) && Number(rent) > 0),
    true,
  ][step];

  const submit = async () => {
    const api = apiFromEnv();
    if (!api || busy) return;
    setBusy(true);
    try {
      const out = await api.opsOnboardOwner({
        owner: existing
          ? { org_id: existing.owner_org_id }
          : { name: ownerName.trim(), phone: phone.trim(), email: email.trim() || undefined },
        property: {
          code: propCode.trim() || propName.trim().split(/\s+/).map((w) => w[0]).join('').toUpperCase(),
          name: propName.trim(), kind,
          address_line1: addressLine1.trim(),
          locality: locality.trim(), city: city.trim(),
          state_code: stateCode.trim().toUpperCase(), pin: pin.trim(),
        },
        units: unitList.map((code) => ({ code, kind: 'flat', carpet_area_sqft: Number(carpetArea) })),
        tenancy: withTenant ? {
          unit_code: tenantUnit.trim(),
          tenant: { name: tenantName.trim(), phone: tenantPhone.trim() },
          start_on: startOn.trim(), end_on: endOn.trim() || undefined,
          rent_amount_minor: Math.round(Number(rent) * 100),
          deposit_amount_minor: Math.round(Number(deposit || 0) * 100),
          due_day: Number(dueDay) || 5,
          deductor_class: ownerIndividual ? 'individual_no_audit' : 'business',
          landlord_residency: ownerResident ? 'resident' : 'non_resident',
        } : undefined,
      });
      setDone(out);
    } catch (err) {
      say((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  if (done) {
    return (
      <>
        <BackHeader title="Welcome aboard" onBack={() => router.back()} />
        <Screen>
          <View style={{ alignItems: 'center', marginTop: space(4) }}><HouseArt size={150} /></View>
          <Card>
            <StatusPill text={done.created_organisation ? 'A brand-new portfolio' : 'Joined their portfolio'} tone="green" dot />
            <Text style={s.h1}>
              {done.created_organisation
                ? `${ownerLabel} is on Dwellm8`
                : `${propName} is in ${ownerLabel}'s books`}
            </Text>
            <View style={{ marginTop: space(3) }}>
              <KeyValue k="Property" v={`${propName}, ${city}`} />
              <KeyValue k="Units" v={String(done.unit_ids?.length ?? 0)} />
              <KeyValue k="Your mandate" v="Full management, portfolio-wide" />
              {done.lease_id ? (
                <KeyValue
                  k="First tenancy"
                  v={done.lease_state === 'active' ? `Active — ${tenantName}` : `Drafted — ${tenantName}`}
                  tone={done.lease_state === 'active' ? 'green' : undefined}
                  last
                />
              ) : (
                <KeyValue k="First tenancy" v="None yet" last />
              )}
            </View>
            {done.lease_note ? <Text style={s.note}>{done.lease_note}</Text> : null}
            <Text style={s.note}>
              {done.created_organisation
                ? `The moment ${ownerLabel.split(' ')[0]} signs into Dwellm8 Own with ${phone}, all of this is already theirs`
                : `${ownerLabel.split(' ')[0]} sees it in Dwellm8 Own already — same books, same mandate`}
              {withTenant && done.lease_state === 'active'
                ? ` — and ${tenantName.split(' ')[0]} sees the tenancy in Dwellm8 Live the same way`
                : withTenant
                  ? `. ${tenantName.split(' ')[0]}'s tenancy appears in Live once it activates`
                  : ''}.
              Nobody sets anything up twice.
            </Text>
          </Card>
          <Button label="Onboard another owner" tone="secondary" onPress={() => { setDone(null); setStep(0); }} style={{ marginHorizontal: space(4) }} />
          <Button label="Back to work" onPress={() => router.back()} style={{ marginHorizontal: space(4), marginTop: space(3) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Onboard an owner" subtitle={STEPS[step]} onBack={() => (step > 0 ? setStep(step - 1) : router.back())} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <View style={s.dots}>
          {STEPS.map((label, i) => (
            <Pressable key={label} onPress={() => i < step && setStep(i)} style={[s.dot, i === step && s.dotNow, i < step && s.dotDone]} />
          ))}
        </View>

        {step === 0 ? (
          <Card>
            <Text style={s.q}>Whose place is it?</Text>
            <Text style={s.sub}>
              An owner you already act for keeps everything in the one set of books. Somebody new
              gets their own, and their mobile number is the whole key — the first time they open
              Dwellm8 Own with it, all of this is already theirs.
            </Text>
            {portfolios.map((p) => (
              <ChoiceRow
                key={p.owner_org_id}
                label={p.owner_name}
                hint={`${p.property_count} ${p.property_count === 1 ? 'property' : 'properties'} under your mandate`}
                selected={existing?.owner_org_id === p.owner_org_id}
                onPress={() => setExisting(existing?.owner_org_id === p.owner_org_id ? null : p)}
              />
            ))}
            <ChoiceRow
              label="Somebody new"
              hint="We'll reserve their identity against their number"
              selected={existing === null}
              onPress={() => setExisting(null)}
              last
            />
            {existing === null ? (
              <>
                <Field label="Owner's name" value={ownerName} onChange={setOwnerName} placeholder="Meera Sharma" />
                <Field label="Mobile number" value={phone} onChange={setPhone} placeholder="+919886021745" keyboardType="phone-pad" />
                <Field label="Email — optional, they can add it later" value={email} onChange={setEmail} placeholder="meera@example.in" keyboardType="email-address" />
              </>
            ) : null}
          </Card>
        ) : null}

        {step === 1 ? (
          <Card>
            <Text style={s.q}>Where's the place?</Text>
            <Text style={s.sub}>These details follow the property everywhere — listings, agreements, receipts.</Text>
            <Field label="Property name" value={propName} onChange={setPropName} placeholder="Brigade Palm Grove" />
            <Field label="Short code — optional" value={propCode} onChange={setPropCode} placeholder="BPG" />
            {kinds.map((k, i) => (
              <ChoiceRow key={k.code} label={k.label} selected={kind === k.code} onPress={() => setKind(k.code)} last={i === kinds.length - 1} />
            ))}
            <Field label="Address" value={addressLine1} onChange={setAddressLine1} placeholder="12 Palm Avenue" />
            <Field label="Locality" value={locality} onChange={setLocality} placeholder="Whitefield" />
            <Field label="City" value={city} onChange={setCity} placeholder="Bengaluru" />
            <Field label="State code — two letters" value={stateCode} onChange={setStateCode} placeholder="KA" autoCapitalize="characters" />
            <Field label="PIN code" value={pin} onChange={setPin} placeholder="560066" keyboardType="numeric" />
          </Card>
        ) : null}

        {step === 2 ? (
          <Card>
            <Text style={s.q}>How is it split?</Text>
            <Text style={s.sub}>
              List the unit codes, comma-separated. Each becomes a lettable unit — a lease, a
              listing and a ledger hang off every one.
            </Text>
            <Field label="Unit codes" value={units} onChange={setUnits} placeholder="101, 102, 201, 202" />
            <Field label="Carpet area, sq ft" value={carpetArea} onChange={setCarpetArea} placeholder="980" keyboardType="numeric" />
            {unitList.length ? (
              <View style={s.chipRow}>
                {unitList.map((u) => (
                  <View key={u} style={s.chip}><Text style={s.chipText}>{u}</Text></View>
                ))}
              </View>
            ) : null}
          </Card>
        ) : null}

        {step === 3 ? (
          <Card>
            <Text style={s.q}>Anyone moving in?</Text>
            <Text style={s.sub}>
              If a tenant is already lined up, name them now and the tenancy starts life with the
              onboarding — rent schedule, receipts and all. Skip it and add tenancies later.
            </Text>
            <SwitchRow label="Set up the first tenancy" hint="Optional" value={withTenant} onChange={setWithTenant} last={!withTenant} />
            {withTenant ? (
              <>
                <Field label="Tenant's name" value={tenantName} onChange={setTenantName} placeholder="Arjun Rao" />
                <Field label="Tenant's mobile" value={tenantPhone} onChange={setTenantPhone} placeholder="+91 99450 12345" keyboardType="phone-pad" />
                <Field label={`Which unit? (${unitList.join(', ') || 'add units first'})`} value={tenantUnit} onChange={setTenantUnit} placeholder={unitList[0] ?? '101'} />
                <Field label="Monthly rent, ₹" value={rent} onChange={setRent} placeholder="42000" keyboardType="numeric" />
                <Field label="Deposit, ₹" value={deposit} onChange={setDeposit} placeholder="126000" keyboardType="numeric" />
                <Field label="Rent due day" value={dueDay} onChange={setDueDay} placeholder="5" keyboardType="numeric" />
                <Field label="Starts on (YYYY-MM-DD)" value={startOn} onChange={setStartOn} placeholder={todayIso()} />
                <Field label="Ends on — 11 months is the usual" value={endOn} onChange={setEndOn} placeholder={plusMonthsIso(11)} />
                <SwitchRow label="The owner is an individual" hint="Not a company or firm — decides the TDS section" value={ownerIndividual} onChange={setOwnerIndividual} />
                <SwitchRow label="The owner lives in India" hint="Tax residency, not citizenship" value={ownerResident} onChange={setOwnerResident} last />
              </>
            ) : null}
          </Card>
        ) : null}

        {step === 4 ? (
          <Card>
            <Text style={s.q}>The once-over</Text>
            <Text style={s.sub}>One look before it becomes the record.</Text>
            <KeyValue k="Owner" v={existing ? `${existing.owner_name} · already yours` : `${ownerName} · ${phone}`} />
            <KeyValue k="Property" v={`${propName}, ${locality}, ${city} ${pin}`} />
            <KeyValue k="Units" v={`${unitList.join(', ')} · ${carpetArea} sq ft each`} />
            {withTenant ? (
              <>
                <KeyValue k="First tenancy" v={`${tenantName} in ${tenantUnit}`} />
                <KeyValue k="Rent" v={`${inr(Math.round(Number(rent || 0) * 100))} · due day ${dueDay}`} />
                <KeyValue k="Term" v={`${startOn} → ${endOn || 'open-ended'}`} />
                <KeyValue k="TDS facts" v={`${ownerIndividual ? 'Individual' : 'Business'}, ${ownerResident ? 'Indian resident' : 'non-resident'}`} last />
              </>
            ) : (
              <KeyValue k="First tenancy" v="Not yet — add one any time" last />
            )}
            <Text style={s.note}>
              {withTenant
                ? 'Confirming here acknowledges the TDS facts on the owner’s behalf — they decide which section governs the rent. '
                : ''}
              Nothing here needs the owner to do anything. Their number claims it all.
            </Text>
          </Card>
        ) : null}
      </Screen>

      <ActionBar>
        {step > 0 ? (
          <Button label="Back" tone="secondary" onPress={() => setStep(step - 1)} style={{ flex: 1 }} />
        ) : null}
        {step < STEPS.length - 1 ? (
          <Button label="Next" onPress={() => setStep(step + 1)} disabled={!stepReady} style={{ flex: 1.6 }} />
        ) : (
          <Button label={busy ? 'Creating…' : 'Make it real'} onPress={submit} disabled={busy || !stepReady} style={{ flex: 1.6 }} />
        )}
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  dots: { flexDirection: 'row', gap: 8, justifyContent: 'center', marginTop: space(4), marginBottom: space(2) },
  dot: { width: 8, height: 8, borderRadius: 4, backgroundColor: '#D5E0EC' },
  dotNow: { backgroundColor: color.accent, width: 22 },
  dotDone: { backgroundColor: color.positive },
  q: { ...font.h2, color: color.inkStrong },
  sub: { ...font.body, color: color.inkSoft, marginTop: space(2), marginBottom: space(3), lineHeight: 21 },
  h1: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  chipRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: space(3) },
  chip: {
    backgroundColor: '#EEF3FC', borderRadius: radius.pill, borderWidth: 1, borderColor: '#DCE5F5',
    paddingHorizontal: 14, paddingVertical: 7,
  },
  chipText: { ...font.label, color: color.accentDeep },
});
