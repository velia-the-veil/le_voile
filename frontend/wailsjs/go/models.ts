export namespace desktop {
	
	export class StatusResponse {
	    status: string;
	    ip: string;
	    country: string;
	    relay_id: string;
	    uptime: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new StatusResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.ip = source["ip"];
	        this.country = source["country"];
	        this.relay_id = source["relay_id"];
	        this.uptime = source["uptime"];
	        this.message = source["message"];
	    }
	}

}

