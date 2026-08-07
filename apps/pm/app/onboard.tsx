import React, { useCallback, useEffect, useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  ActionBar, AddressLookup, BackHeader, Button, Card, ChoiceRow, Field, HouseArt, KeyValue,
  Screen, StatusPill, SwitchRow, Toast,
  apiFromEnv, color, font, inr, radius, space,
  type AddressSuggestion,
} from '@dwellm8/mobile-shared';
import type { OpsPortfolio, OwnerOnboarded, TaxProfile } from '@dwellm8/mobile-shared';
import { useFirmContact } from '../src/data/firm';
import { CaptureRefused, photographDocument } from '../src/data/capture';
import {
  emptyIdentity, identityReady, missingRule37BC, prefillFromPAN, prefillFromPassport,
  taxProfileAction, taxProfileFrom, taxSummary, type IdentityDraft,
} from '../src/data/identity';
import { fileCopy, type Copy } from '../src/data/papers';

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

const copyNames: Partial<Record<Copy['kind'], string>> = {
  pan_card: 'The PAN card',
  passport: 'The passport',
};

const STEPS = ['The owner', 'Who they are', 'The place', 'The units', 'Moving in', 'The once-over'] as const;

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
  const firm = useFirmContact();

  // The owner — one the firm already acts for, or somebody new
  const [portfolios, setPortfolios] = useState<OpsPortfolio[]>([]);
  const [existing, setExisting] = useState<OpsPortfolio | null>(null);
  // The solo manager: the property is theirs, so there is no mandate (#268).
  const [mine, setMine] = useState(false);
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
  // Who they are, for TDS — the identifiers travel once and are stored masked
  // or as a boolean by the endpoint that receives them (#318, ADR-0013).
  const [ident, setIdent] = useState<IdentityDraft>(emptyIdentity());
  const [onFile, setOnFile] = useState<TaxProfile | null>(null);
  const [scanning, setScanning] = useState<'pan' | 'passport' | null>(null);
  // The copies photographed on the way through. They cannot be filed until the
  // owner exists, so they wait here — nothing is written to the device.
  const [copies, setCopies] = useState<Copy[]>([]);
  const [attested, setAttested] = useState(true);
  const keep = (c: Copy) => setCopies((held) => [...held.filter((h) => h.kind !== c.kind), c]);

  const unitList = units.split(',').map((c) => c.trim()).filter(Boolean);

  const findAddress = useCallback(async (q: string) => {
    const client = apiFromEnv();
    return client ? client.searchAddresses(q) : [];
  }, []);

  // The address a listing, an agreement and every receipt will carry: picked
  // from the map, not typed from memory (#293).
  const takeAddress = useCallback((a: AddressSuggestion) => {
    setAddressLine1(a.line1 || a.description);
    if (a.locality) setLocality(a.locality);
    if (a.city) setCity(a.city);
    if (a.state_code) setStateCode(a.state_code);
    if (a.pin_code) setPin(a.pin_code);
  }, []);

  useEffect(() => {
    const api = apiFromEnv();
    if (!api) return;
    api.opsPortfolios().then(setPortfolios).catch(() => setPortfolios([]));
  }, []);

  // An owner the firm already acts for may have furnished this months ago.
  // Show what is on file rather than asking for it a second time.
  useEffect(() => {
    const api = apiFromEnv();
    const party = existing?.owner_party_id;
    if (!api || !party) { setOnFile(null); return; }
    api.opsTaxProfile(party)
      .then((p) => {
        setOnFile(p);
        setIdent((d) => ({
          ...d, resident: p.residency === 'resident',
          individual: p.payee_form !== 'company',
          country: p.residency === 'resident' ? '' : p.residence_country,
        }));
      })
      .catch(() => setOnFile(null));
  }, [existing?.owner_party_id]);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 3200);
  };

  const ownerLabel = existing ? existing.owner_name : ownerName;

  // Read the card, then hand back what it says for the owner to confirm. A
  // document that did not read cleanly is refused by the API rather than
  // half-prefilled, so there is nothing to undo here.
  const scan = async (kind: 'pan' | 'passport') => {
    const api = apiFromEnv();
    if (!api || scanning) return;
    setScanning(kind);
    try {
      const shot = await photographDocument();
      if (kind === 'pan') {
        const card = await api.opsScanPAN(shot.base64);
        setIdent(prefillFromPAN(ident, card));
        keep({ kind: 'pan_card', uri: shot.uri, contentType: shot.contentType,
          selfAttested: attested, number: card.number, issuingCountry: 'IN' });
      } else {
        const passport = await api.opsScanPassport(shot.base64);
        setIdent(prefillFromPassport(ident, passport));
        // No issuing country: the MRZ carries ICAO's three-letter code and the
        // field is ISO's two. Guessing between them puts a wrong country on file.
        keep({ kind: 'passport', uri: shot.uri, contentType: shot.contentType,
          selfAttested: attested, number: passport.number, expiresOn: passport.expires_on });
      }
    } catch (err) {
      if (!(err instanceof CaptureRefused)) say((err as Error).message);
    } finally {
      setScanning(null);
    }
  };

  const takeScannedName = () => {
    if (ident.scannedName) setOwnerName(ident.scannedName);
    setIdent((d) => ({ ...d, scannedName: undefined }));
  };

  const shortfall = missingRule37BC(ident);
  // What the wizard may do with what it collected, against what the owner
  // already furnished (#319).
  const taxAction = taxProfileAction(ident, onFile);

  const stepReady = [
    existing !== null || (ownerName.trim().length > 1 && phone.trim().length >= 10),
    identityReady(ident) && taxAction.kind !== 'needs-pan',
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
          : {
            name: ownerName.trim(), phone: phone.trim(),
            email: email.trim() || undefined, self: mine || undefined,
          },
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
          deductor_class: ident.individual ? 'individual_no_audit' : 'business',
          landlord_residency: ident.resident ? 'resident' : 'non_resident',
        } : undefined,
      });
      // The profile is written against the party, which only exists once the
      // onboarding lands. It failing does not undo any of the above — the
      // owner is on Dwellm8 either way, and the rate can be furnished later.
      if (taxAction.kind === 'record') {
        try {
          await api.opsRecordTaxProfile(out.owner_party_id, taxProfileFrom(ident, startOn.trim() || todayIso()));
        } catch {
          say('Onboarded — but what they furnished for TDS did not save. Add it from their profile.');
        }
      }
      // The copies behind it, for the same reason and with the same fallback:
      // an owner on Dwellm8 with no passport on file is a chase, not an outage.
      for (const copy of copies) {
        try {
          await fileCopy(api, out.owner_party_id, copy, todayIso());
        } catch {
          say('Onboarded — but a copy did not upload. Attach it from their profile.');
        }
      }
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
              <KeyValue k="Your mandate" v={done.grant_id ? 'Full management, portfolio-wide' : 'Your own property — no mandate needed'} />
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
                label={p.self_managed ? `${p.owner_name} — your own property` : p.owner_name}
                hint={p.self_managed
                  ? `${p.property_count} ${p.property_count === 1 ? 'property' : 'properties'} you own yourself`
                  : `${p.property_count} ${p.property_count === 1 ? 'property' : 'properties'} under your mandate`}
                selected={existing?.owner_org_id === p.owner_org_id}
                onPress={() => {
                  setMine(false);
                  setExisting(existing?.owner_org_id === p.owner_org_id ? null : p);
                }}
              />
            ))}
            <ChoiceRow
              label="It's mine"
              hint="You own it and you manage it — your own books, no mandate between"
              selected={existing === null && mine}
              onPress={() => {
                setExisting(null);
                setMine(true);
                // The register already holds who you are — don't ask again (#295).
                setOwnerName((v) => v || firm.name);
                setPhone((v) => v || firm.phone);
              }}
            />
            <ChoiceRow
              label="Somebody new"
              hint="We'll reserve their identity against their number"
              selected={existing === null && !mine}
              onPress={() => { setExisting(null); setMine(false); }}
              last
            />
            {existing === null ? (
              <>
                <Field label={mine ? 'Your name' : "Owner's name"} value={ownerName} onChange={setOwnerName} placeholder="Meera Sharma" autoCapitalize="words" />
                <Field
                  label={mine ? 'Your mobile number' : 'Mobile number'}
                  value={phone} onChange={setPhone} placeholder="+919886021745" keyboardType="phone-pad"
                />
                <Field label="Email — optional, they can add it later" value={email} onChange={setEmail} placeholder="meera@example.in" keyboardType="email-address" />
              </>
            ) : null}
          </Card>
        ) : null}

        {step === 1 ? (
          <Card>
            <Text style={s.q}>Who are they, for tax?</Text>
            <Text style={s.sub}>
              This is what decides the rate their rent is deducted at, so it is worth a minute.
              Photograph the card and Dwellm8 reads it — nothing is stored as you see it here:
              the PAN becomes a yes, and a foreign tax number keeps only its last four digits.
            </Text>

            {onFile ? (
              <View style={{ marginBottom: space(3) }}>
                <StatusPill text={`On file since ${onFile.valid_from}`} tone="green" dot />
                <KeyValue k="Residency" v={onFile.residency === 'resident' ? 'Indian resident' : `Non-resident · ${onFile.residence_country}`} />
                <KeyValue k="PAN" v={onFile.pan_furnished ? 'Furnished' : 'Not furnished — 20% applies'} last />
                <Text style={s.note}>Anything you change here is furnished afresh from today; what was true before stays true for the months it covered.</Text>
                {taxAction.kind === 'needs-pan' ? (
                  <>
                    <StatusPill text="The PAN has to be entered again" tone="amber" dot />
                    <Text style={s.note}>
                      A fresh declaration replaces this one whole, and the PAN on file cannot be
                      carried across — enter it again, or leave the rest as it was.
                    </Text>
                  </>
                ) : null}
              </View>
            ) : null}

            <SwitchRow
              label="The owner lives in India"
              hint="Tax residency, not citizenship — it selects section 194-I or 195"
              value={ident.resident}
              onChange={(v) => setIdent((d) => ({ ...d, resident: v }))}
            />
            <SwitchRow
              label="The owner is an individual"
              hint="Not a company or a firm. The PAN's fourth letter says which"
              value={ident.individual}
              onChange={(v) => setIdent((d) => ({ ...d, individual: v }))}
            />
            <SwitchRow
              label="The owner signed and dated the copy"
              hint="Turn this off if you photographed the original in front of you"
              value={attested}
              onChange={setAttested}
              last
            />

            <Button
              label={scanning === 'pan' ? 'Reading the card…' : 'Photograph the PAN card'}
              tone="secondary" disabled={scanning !== null} onPress={() => scan('pan')}
              style={{ marginTop: space(3) }}
            />
            <Field
              label="PAN — leave it empty if they have none"
              value={ident.pan} onChange={(v) => setIdent((d) => ({ ...d, pan: v }))}
              placeholder="ABCPD1234E" autoCapitalize="characters"
            />
            {ident.pan.trim() === '' ? (
              <Text style={s.note}>
                Without a PAN, section 206AA deducts at 20% however the tenancy is written.
              </Text>
            ) : null}

            {ident.scannedName ? (
              <Pressable onPress={takeScannedName} style={s.prefill}>
                <Text style={s.prefillText}>The document reads {ident.scannedName} — use that name</Text>
              </Pressable>
            ) : null}
            {ident.warning ? <Text style={s.warn}>{ident.warning}</Text> : null}
            {copies.length ? (
              <Text style={s.note}>
                {copies.map((c) => copyNames[c.kind] ?? c.kind).join(' and ')} will be filed
                against them once they exist.
              </Text>
            ) : null}

            {!ident.resident ? (
              <>
                <Text style={[s.q, { fontSize: 17, marginTop: space(4) }]}>Living abroad</Text>
                <Text style={s.sub}>
                  Rent to a non-resident is deducted under section 195, and without a PAN that
                  means 20% — unless all five of rule 37BC(2)'s particulars are furnished. Take
                  them off the passport and the certificate now and the rate follows.
                </Text>
                <Button
                  label={scanning === 'passport' ? 'Reading the passport…' : 'Photograph the passport'}
                  tone="secondary" disabled={scanning !== null} onPress={() => scan('passport')}
                />
                <Field
                  label="Country of residence — two letters"
                  value={ident.country} onChange={(v) => setIdent((d) => ({ ...d, country: v }))}
                  placeholder="AE" autoCapitalize="characters"
                />
                <Field
                  label="Foreign tax identification number"
                  value={ident.foreignTin} onChange={(v) => setIdent((d) => ({ ...d, foreignTin: v }))}
                  placeholder="784197412345671"
                />
                <Field
                  label="Tax residency certificate number"
                  value={ident.trcNumber} onChange={(v) => setIdent((d) => ({ ...d, trcNumber: v }))}
                  placeholder="TRC-AE-2026-0041"
                />
                <Field
                  label="Certificate valid from (YYYY-MM-DD)"
                  value={ident.trcValidFrom} onChange={(v) => setIdent((d) => ({ ...d, trcValidFrom: v }))}
                  placeholder="2026-04-01"
                />
                <Field
                  label="Certificate valid to"
                  value={ident.trcValidTo} onChange={(v) => setIdent((d) => ({ ...d, trcValidTo: v }))}
                  placeholder="2027-03-31"
                />
                <Field
                  label="Address abroad"
                  value={ident.foreignAddress} onChange={(v) => setIdent((d) => ({ ...d, foreignAddress: v }))}
                  placeholder="Villa 12, Al Barsha, Dubai"
                />
                <Field
                  label="Email abroad"
                  value={ident.foreignEmail} onChange={(v) => setIdent((d) => ({ ...d, foreignEmail: v }))}
                  placeholder="anjali@example.ae" keyboardType="email-address"
                />
                <Field
                  label="Phone abroad"
                  value={ident.foreignPhone} onChange={(v) => setIdent((d) => ({ ...d, foreignPhone: v }))}
                  placeholder="+971 50 000 0001" keyboardType="phone-pad"
                />
                {shortfall.length ? (
                  <Text style={s.warn}>
                    Still missing {shortfall.join(', ')}. Until all five are furnished, a
                    non-resident without a PAN is deducted at 20%.
                  </Text>
                ) : (
                  <Text style={s.note}>
                    All five particulars are furnished — rule 37BC(2) is satisfied and the
                    treaty rate governs instead of 206AA's floor.
                  </Text>
                )}
              </>
            ) : null}
          </Card>
        ) : null}

        {step === 2 ? (
          <Card>
            <Text style={s.q}>Where's the place?</Text>
            <Text style={s.sub}>These details follow the property everywhere — listings, agreements, receipts.</Text>
            <Field label="Property name" value={propName} onChange={setPropName} placeholder="Brigade Palm Grove" />
            <Field label="Short code — optional" value={propCode} onChange={setPropCode} placeholder="BPG" />
            {kinds.map((k, i) => (
              <ChoiceRow key={k.code} label={k.label} selected={kind === k.code} onPress={() => setKind(k.code)} last={i === kinds.length - 1} />
            ))}
            <AddressLookup search={findAddress} onPick={takeAddress} />
            <Field label="Address" value={addressLine1} onChange={setAddressLine1} placeholder="12 Palm Avenue" />
            <Field label="Locality" value={locality} onChange={setLocality} placeholder="Whitefield" />
            <Field label="City" value={city} onChange={setCity} placeholder="Bengaluru" />
            <Field label="State code — two letters" value={stateCode} onChange={setStateCode} placeholder="KA" autoCapitalize="characters" />
            <Field label="PIN code" value={pin} onChange={setPin} placeholder="560066" keyboardType="numeric" />
          </Card>
        ) : null}

        {step === 3 ? (
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

        {step === 4 ? (
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
              </>
            ) : null}
          </Card>
        ) : null}

        {step === 5 ? (
          <Card>
            <Text style={s.q}>The once-over</Text>
            <Text style={s.sub}>One look before it becomes the record.</Text>
            <KeyValue k="Owner" v={existing ? `${existing.owner_name} · already yours` : `${ownerName} · ${phone}`} />
            <KeyValue
              k="For tax"
              v={taxSummary(ident, onFile)}
            />
            {!ident.resident && shortfall.length ? (
              <KeyValue k="Rule 37BC" v={`Missing ${shortfall.length} of 5 particulars`} tone="amber" />
            ) : null}
            <KeyValue k="Property" v={`${propName}, ${locality}, ${city} ${pin}`} />
            <KeyValue k="Units" v={`${unitList.join(', ')} · ${carpetArea} sq ft each`} />
            {withTenant ? (
              <>
                <KeyValue k="First tenancy" v={`${tenantName} in ${tenantUnit}`} />
                <KeyValue k="Rent" v={`${inr(Math.round(Number(rent || 0) * 100))} · due day ${dueDay}`} />
                <KeyValue
                  k="Deposit"
                  v={Number(deposit) > 0 ? inr(Math.round(Number(deposit) * 100)) : 'None taken'}
                />
                <KeyValue k="Term" v={`${startOn} → ${endOn || 'open-ended'}`} />
                <KeyValue k="TDS facts" v={`${ident.individual ? 'Individual' : 'Business'}, ${ident.resident ? 'Indian resident' : `non-resident · ${ident.country}`}`} last />
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
  prefill: {
    backgroundColor: '#EEF3FC', borderRadius: radius.md, borderWidth: 1, borderColor: '#DCE5F5',
    padding: space(3), marginTop: space(3),
  },
  prefillText: { ...font.label, color: color.accentDeep },
  warn: { ...font.small, color: color.negative, marginTop: space(3), lineHeight: 18 },
});
