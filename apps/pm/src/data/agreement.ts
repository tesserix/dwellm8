import type { DwellmApi } from '@dwellm8/mobile-shared';

/**
 * The owner–manager agreement (#340): the manager may enter and manage, may
 * not sell or deal, and an owner intending to sell gives four months' notice.
 * Nothing is signed online yet — this prints, both sides sign paper, and the
 * executed copy is filed back as a management_agreement document (#339).
 */
export type AgreementQuestion = {
  field: string;
  label: string;
  hint?: string;
  keyboard?: 'numeric';
};

const labels: Record<string, { label: string; hint?: string; keyboard?: 'numeric' }> = {
  owner_name: { label: 'Owner name' },
  owner_address: { label: 'Owner address' },
  owner_pan: { label: 'Owner PAN', hint: 'Ten characters, as on the card' },
  manager_name: { label: 'Your firm' },
  manager_address: { label: 'Firm address' },
  manager_rera: { label: 'RERA registration' },
  property_description: { label: 'Property description', hint: 'Two-bedroom flat, 1,150 sq ft' },
  term_months: { label: 'Term (months)', keyboard: 'numeric' },
  commencement_date: { label: 'Starts on', hint: 'YYYY-MM-DD' },
  management_fee_pct: { label: 'Management fee (%)', keyboard: 'numeric' },
  repair_ceiling: { label: 'Repairs without approval (Rs.)', keyboard: 'numeric' },
  execution_place: { label: 'Signed at' },
  execution_date: { label: 'Signed on', hint: 'YYYY-MM-DD' },
};

const describe = (field: string) => labels[field] ?? { label: field.replace(/_/g, ' ') };

/** What the firm must fill. The server names the fields, so a template
 * revision cannot leave the app asking for the wrong ones. */
export async function agreementQuestions(api: DwellmApi): Promise<AgreementQuestion[]> {
  const { fields } = await api.opsManagementAgreementFields();
  return fields.map((field) => ({ field, ...describe(field) }));
}

export type PrintedAgreement = {
  filename: string;
  documentId?: string;
  downloadUrl?: string;
};

export async function printAgreement(api: DwellmApi, propertyId: string,
  answers: Record<string, string>): Promise<PrintedAgreement> {
  const filled: Record<string, string> = {};
  for (const [field, value] of Object.entries(answers)) {
    // A clause printed with a placeholder still in it is a figure somebody
    // signs believing it is there.
    if (!value.trim()) {
      throw new Error(`${describe(field).label} is still blank`);
    }
    filled[field] = value.trim();
  }

  const out = await api.opsPrintManagementAgreement(propertyId, filled);
  return { filename: out.filename, documentId: out.document_id, downloadUrl: out.download_url };
}
